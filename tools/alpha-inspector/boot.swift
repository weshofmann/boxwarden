import Foundation
import Virtualization

private enum BootFailure: Error, CustomStringConvertible {
    case startTimeout
    case stopTimeout
    case serialTimeout
    case streamOverflow
    case unexpectedState

    var description: String {
        switch self {
        case .startTimeout: return "VM start callback did not arrive within 15 seconds"
        case .stopTimeout: return "VM did not reach stopped state within bounded stop sequence"
        case .serialTimeout: return "serial pipes did not reach EOF after VM stop"
        case .streamOverflow: return "guest serial output exceeded fixed byte limit"
        case .unexpectedState: return "VM state or network-device count changed"
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
    let output: FileHandle?
    let maximum: Int
    let done = DispatchSemaphore(value: 0)
    var count = 0
    var overflow = false
    var error: Error?

    init(input: FileHandle, output: FileHandle?, maximum: Int) {
        self.input = input
        self.output = output
        self.maximum = maximum
    }

    func start() {
        DispatchQueue.global(qos: .utility).async {
            defer { self.done.signal() }
            do {
                while let chunk = try self.input.read(upToCount: 4096), !chunk.isEmpty {
                    self.count += chunk.count
                    if self.count > self.maximum {
                        self.overflow = true
                        continue // Drain to avoid blocking guest shutdown.
                    }
                    self.output?.write(chunk)
                }
            } catch {
                self.error = error
            }
        }
    }
}

private func requestAndForceStop(_ vm: VZVirtualMachine, queue: DispatchQueue, observer: StopObserver) throws {
    if queue.sync(execute: { vm.state == .stopped }) { return }
    queue.async {
        if vm.canRequestStop {
            try? vm.requestStop()
        }
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
        throw BootFailure.stopTimeout
    }
    if !queue.sync(execute: { vm.state == .stopped }) {
        guard observer.stopped.wait(timeout: .now() + .seconds(5)) == .success,
              queue.sync(execute: { vm.state == .stopped }) else {
            throw BootFailure.stopTimeout
        }
    }
}

// The only VM-starting path. stdout carries guest serial bytes exclusively;
// stderr receives bounded machine-readable host observations. A process exit
// is not treated as success unless the VM is stopped and both pipes reach EOF.
func bootProof(_ prepared: PreparedConfiguration) throws {
    if let expected = prepared.exportDisk {
        try requireExportSnapshotDisk(prepared.diskURL, expected: expected)
    }
    let queue = DispatchQueue(label: "boxwarden.alpha.inspector.vm")
    let observer = StopObserver()
    let vm = queue.sync {
        let vm = VZVirtualMachine(configuration: prepared.configuration, queue: queue)
        vm.delegate = observer
        return vm
    }
    guard queue.sync(execute: { vm.networkDevices.isEmpty && vm.state == .stopped }) else {
        throw BootFailure.unexpectedState
    }

    let consolePump = SerialPump(input: prepared.consoleOutput.fileHandleForReading, output: nil, maximum: 256 * 1024)
    let exportPump = SerialPump(input: prepared.exportOutput.fileHandleForReading, output: .standardOutput,
                                maximum: prepared.exportDisk == nil ? 64 * 1024 : 320 * 1024 * 1024)
    consolePump.start()
    exportPump.start()
    defer {
        if !queue.sync(execute: { vm.state == .stopped }) {
            try? requestAndForceStop(vm, queue: queue, observer: observer)
        }
        try? prepared.consoleOutput.fileHandleForWriting.close()
        try? prepared.exportOutput.fileHandleForWriting.close()
    }

    let started = DispatchSemaphore(value: 0)
    var startError: Error?
    queue.async {
        vm.start { result in
            if case let .failure(error) = result {
                startError = error
            }
            started.signal()
        }
    }
    guard started.wait(timeout: .now() + .seconds(15)) == .success else {
        throw BootFailure.startTimeout
    }
    if let startError { throw startError }

    let guestDeadline = prepared.exportDisk == nil ? 30 : 50
    if observer.stopped.wait(timeout: .now() + .seconds(guestDeadline)) == .timedOut {
        try requestAndForceStop(vm, queue: queue, observer: observer)
        throw BootFailure.stopTimeout
    }
    if let stopError = observer.error { throw stopError }
    let (vmStopped, networkDeviceCount) = queue.sync { (vm.state == .stopped, vm.networkDevices.count) }
    guard vmStopped, networkDeviceCount == 0 else {
        throw BootFailure.unexpectedState
    }

    try prepared.consoleOutput.fileHandleForWriting.close()
    try prepared.exportOutput.fileHandleForWriting.close()
    guard consolePump.done.wait(timeout: .now() + .seconds(5)) == .success,
          exportPump.done.wait(timeout: .now() + .seconds(5)) == .success else {
        throw BootFailure.serialTimeout
    }
    if let error = consolePump.error ?? exportPump.error { throw error }
    if consolePump.overflow || exportPump.overflow { throw BootFailure.streamOverflow }
    if let expected = prepared.exportDisk {
        try requireExportSnapshotDisk(prepared.diskURL, expected: expected)
    }

    var evidence: [String: Any] = [
        "vm_state": "stopped",
        "runtime_network_devices": networkDeviceCount,
        "console_bytes": consolePump.count,
        "export_bytes": exportPump.count,
    ]
    if prepared.exportDisk != nil { evidence["inspector_mode"] = "export" }
    let encoded = try JSONSerialization.data(withJSONObject: evidence, options: [.sortedKeys])
    FileHandle.standardError.write(Data("BOOT_EVIDENCE ".utf8))
    FileHandle.standardError.write(encoded)
    FileHandle.standardError.write(Data([0x0a]))
}
