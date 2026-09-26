import Foundation
import Virtualization

private enum RunFailure: Error, CustomStringConvertible {
    case startTimeout
    case stopTimeout
    case serialTimeout
    case serialOverflow
    case invalidGuestReport
    case unexpectedState

    var description: String {
        switch self {
        case .startTimeout: return "formatter VM start did not complete within 15 seconds"
        case .stopTimeout: return "formatter VM did not stop within the bounded interval"
        case .serialTimeout: return "formatter serial streams did not drain after stop"
        case .serialOverflow: return "formatter serial output exceeded its fixed bound"
        case .invalidGuestReport: return "formatter guest report is absent, malformed, or unsuccessful"
        case .unexpectedState: return "formatter VM state or isolation configuration changed"
        }
    }
}

private final class StopObserver: NSObject, VZVirtualMachineDelegate {
    let stopped = DispatchSemaphore(value: 0)
    var error: Error?

    func guestDidStop(_ virtualMachine: VZVirtualMachine) {
        stopped.signal()
    }

    func virtualMachine(_ virtualMachine: VZVirtualMachine, didStopWithError error: Error) {
        self.error = error
        stopped.signal()
    }
}

private final class SerialPump {
    let input: FileHandle
    let maximum: Int
    let retain: Bool
    let done = DispatchSemaphore(value: 0)
    var bytes = Data()
    var count = 0
    var overflow = false
    var error: Error?

    init(input: FileHandle, maximum: Int, retain: Bool) {
        self.input = input
        self.maximum = maximum
        self.retain = retain
    }

    func start() {
        DispatchQueue.global(qos: .utility).async {
            defer { self.done.signal() }
            do {
                while let chunk = try self.input.read(upToCount: 4096), !chunk.isEmpty {
                    self.count += chunk.count
                    if self.count > self.maximum {
                        self.overflow = true
                        continue // Drain even after overflow to allow VM shutdown.
                    }
                    if self.retain { self.bytes.append(chunk) }
                }
            } catch {
                self.error = error
            }
        }
    }
}

private func forceStop(_ vm: VZVirtualMachine, queue: DispatchQueue, observer: StopObserver) throws {
    if queue.sync(execute: { vm.state == .stopped }) { return }
    queue.async {
        if vm.canRequestStop { try? vm.requestStop() }
    }
    if observer.stopped.wait(timeout: .now() + .seconds(3)) == .success,
       queue.sync(execute: { vm.state == .stopped }) {
        return
    }
    let forced = DispatchSemaphore(value: 0)
    queue.async {
        if vm.canStop {
            vm.stop { _ in forced.signal() }
        } else {
            forced.signal()
        }
    }
    guard forced.wait(timeout: .now() + .seconds(5)) == .success else {
        throw RunFailure.stopTimeout
    }
    if !queue.sync(execute: { vm.state == .stopped }) {
        guard observer.stopped.wait(timeout: .now() + .seconds(5)) == .success,
              queue.sync(execute: { vm.state == .stopped }) else {
            throw RunFailure.stopTimeout
        }
    }
}

private struct GuestEvidence: Decodable {
    let observedUUID: String
    let wholeDevice: Bool
    let filesystemClean: Bool

    enum CodingKeys: String, CodingKey {
        case observedUUID = "observed_uuid"
        case wholeDevice = "whole_device"
        case filesystemClean = "filesystem_clean"
    }
}

private struct GuestReport: Decodable {
    let version: Int
    let transaction: String
    let success: Bool
    let evidence: GuestEvidence?
    let error: String?
}

private struct RunEvidence: Encodable {
    let version = 1
    let transaction: String
    let vmStopped = true
    let runtimeNetworkDevices = 0
    let storageDevices = 1
    let storageReadOnly = false
    let serialPorts = 2
    let socketDevices = 0
    let sharedDirectoryDevices = 0
    let observedUUID: String
    let wholeDevice = true
    let filesystemClean = true

    enum CodingKeys: String, CodingKey {
        case version
        case transaction
        case vmStopped = "vm_stopped"
        case runtimeNetworkDevices = "runtime_network_devices"
        case storageDevices = "storage_devices"
        case storageReadOnly = "storage_read_only"
        case serialPorts = "serial_ports"
        case socketDevices = "socket_devices"
        case sharedDirectoryDevices = "shared_directory_devices"
        case observedUUID = "observed_uuid"
        case wholeDevice = "whole_device"
        case filesystemClean = "filesystem_clean"
    }
}

func runFormatterVM(_ prepared: PreparedFormatter) throws {
    let queue = DispatchQueue(label: "boxwarden.alpha.formatter.vm")
    let observer = StopObserver()
    let vm = queue.sync {
        let vm = VZVirtualMachine(configuration: prepared.configuration, queue: queue)
        vm.delegate = observer
        return vm
    }
    guard queue.sync(execute: { vm.state == .stopped && vm.networkDevices.isEmpty }),
          prepared.configuration.storageDevices.count == 1,
          !prepared.diskAttachment.isReadOnly else {
        throw RunFailure.unexpectedState
    }
    try prepared.recheckDisk()
    try prepared.recheckJournal()

    let console = SerialPump(input: prepared.consoleOutput.fileHandleForReading, maximum: 256 * 1024, retain: false)
    let report = SerialPump(input: prepared.reportOutput.fileHandleForReading, maximum: 4096, retain: true)
    console.start()
    report.start()
    defer {
        if !queue.sync(execute: { vm.state == .stopped }) {
            do {
                try forceStop(vm, queue: queue, observer: observer)
            } catch {
                fputs("alpha-formatter cleanup ambiguous: \(error)\n", stderr)
            }
        }
        try? prepared.consoleOutput.fileHandleForWriting.close()
        try? prepared.reportOutput.fileHandleForWriting.close()
    }

    let started = DispatchSemaphore(value: 0)
    var startError: Error?
    queue.async {
        vm.start { result in
            if case let .failure(error) = result { startError = error }
            started.signal()
        }
    }
    guard started.wait(timeout: .now() + .seconds(15)) == .success else {
        throw RunFailure.startTimeout
    }
    if let startError { throw startError }
    if observer.stopped.wait(timeout: .now() + .seconds(120)) == .timedOut {
        try forceStop(vm, queue: queue, observer: observer)
        throw RunFailure.stopTimeout
    }
    if let stopError = observer.error { throw stopError }
    guard queue.sync(execute: { vm.state == .stopped && vm.networkDevices.isEmpty }) else {
        throw RunFailure.unexpectedState
    }
    try prepared.consoleOutput.fileHandleForWriting.close()
    try prepared.reportOutput.fileHandleForWriting.close()
    guard console.done.wait(timeout: .now() + .seconds(5)) == .success,
          report.done.wait(timeout: .now() + .seconds(5)) == .success else {
        throw RunFailure.serialTimeout
    }
    if let error = console.error ?? report.error { throw error }
    if console.overflow || report.overflow { throw RunFailure.serialOverflow }
    let bytes = report.bytes
    guard bytes.count > 1, bytes.last == 0x0a,
          !bytes.dropLast().contains(0x0a),
          let guest = try? JSONDecoder().decode(GuestReport.self, from: bytes),
          guest.version == 1,
          guest.transaction == prepared.arguments.transaction,
          guest.success, guest.error == nil,
          let evidence = guest.evidence,
          evidence.observedUUID == prepared.arguments.filesystemUUID,
          evidence.wholeDevice, evidence.filesystemClean else {
        throw RunFailure.invalidGuestReport
    }
    try prepared.recheckDisk()
    try prepared.recheckJournal()
    let result = RunEvidence(transaction: guest.transaction, observedUUID: evidence.observedUUID)
    let encoded = try JSONEncoder().encode(result)
    FileHandle.standardOutput.write(encoded)
    FileHandle.standardOutput.write(Data([0x0a]))
}
