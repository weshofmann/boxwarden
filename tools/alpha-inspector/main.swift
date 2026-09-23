import Foundation
import Virtualization

private struct ProbeEvidence: Encodable {
    let validated: Bool
    let networkDevices: Int
    let runtimeNetworkDevices: Int
    let storageDevices: Int
    let storageReadOnly: Bool
    let serialPorts: Int
    let socketDevices: Int
    let sharedDirectoryDevices: Int
    let vmState: String

    enum CodingKeys: String, CodingKey {
        case validated
        case networkDevices = "network_devices"
        case runtimeNetworkDevices = "runtime_network_devices"
        case storageDevices = "storage_devices"
        case storageReadOnly = "storage_read_only"
        case serialPorts = "serial_ports"
        case socketDevices = "socket_devices"
        case sharedDirectoryDevices = "shared_directory_devices"
        case vmState = "vm_state"
    }
}

private enum ProbeFailure: Error, CustomStringConvertible {
    case usage
    case unsafeDisk
    case unexpectedConfiguration
    case invalidTransaction

    var description: String {
        switch self {
        case .usage:
            return "usage: alpha-inspector probe <kernel> <initrd> <synthetic.raw> | preflight|boot-probe|preflight-ext4|boot-ext4 <kernel> <initrd> <synthetic.raw> <transaction-hex>"
        case .unsafeDisk:
            return "probe accepts only a fixed 8 MiB zero or 64 MiB ext4 synthetic.raw under a /private/tmp/boxwarden-inspector-probe.* or boxwarden-inspector-boot.* directory"
        case .unexpectedConfiguration:
            return "Virtualization configuration is not the fixed no-NIC probe configuration"
        case .invalidTransaction:
            return "boot probe requires one nonzero lowercase 16-byte transaction hex value"
        }
    }
}

private func requireSyntheticDisk(_ url: URL) throws {
    let normalized = url.standardizedFileURL
    let parent = normalized.deletingLastPathComponent()
    let tempRoot = parent.deletingLastPathComponent().path
    guard normalized.lastPathComponent == "synthetic.raw",
          (parent.lastPathComponent.hasPrefix("boxwarden-inspector-probe.") ||
           parent.lastPathComponent.hasPrefix("boxwarden-inspector-boot.")),
          tempRoot == "/private/tmp" || tempRoot == "/tmp" else {
        throw ProbeFailure.unsafeDisk
    }
    let parentValues = try parent.resourceValues(forKeys: [.isDirectoryKey, .isSymbolicLinkKey])
    guard parentValues.isDirectory == true, parentValues.isSymbolicLink != true else {
        throw ProbeFailure.unsafeDisk
    }
}

struct PreparedConfiguration {
    let configuration: VZVirtualMachineConfiguration
    let diskAttachment: VZDiskImageStorageDeviceAttachment
    let consoleOutput: Pipe
    let exportOutput: Pipe
}

func prepareConfiguration(kernelURL: URL, initrdURL: URL, diskURL: URL, transactionHex: String?, ext4Fixture: Bool = false) throws -> PreparedConfiguration {
    try requireSyntheticDisk(diskURL)
    let diskValues = try diskURL.resourceValues(forKeys: [.isRegularFileKey, .isSymbolicLinkKey, .fileSizeKey])
    guard diskValues.isRegularFile == true,
          diskValues.isSymbolicLink != true,
          diskValues.fileSize == (ext4Fixture ? 64 : 8) * 1024 * 1024 else {
        throw ProbeFailure.unsafeDisk
    }

    let configuration = VZVirtualMachineConfiguration()
    configuration.cpuCount = 2
    configuration.memorySize = 2 * 1024 * 1024 * 1024
    configuration.platform = VZGenericPlatformConfiguration()

    let bootLoader = VZLinuxBootLoader(kernelURL: kernelURL)
    bootLoader.initialRamdiskURL = initrdURL
    if let transactionHex {
        guard transactionHex.count == 32,
              transactionHex != String(repeating: "0", count: 32),
              transactionHex.utf8.allSatisfy({ ($0 >= 48 && $0 <= 57) || ($0 >= 97 && $0 <= 102) }) else {
            throw ProbeFailure.invalidTransaction
        }
        let selector = ext4Fixture ? " alpha_fixture=ext4" : ""
        bootLoader.commandLine = "console=hvc0 rdinit=/alpha-probe alpha_tx=\(transactionHex)\(selector)"
    } else {
        bootLoader.commandLine = "console=hvc0"
    }
    configuration.bootLoader = bootLoader

    let diskAttachment = try VZDiskImageStorageDeviceAttachment(url: diskURL, readOnly: true)
    let blockDevice = VZVirtioBlockDeviceConfiguration(attachment: diskAttachment)
    configuration.storageDevices = [blockDevice]

    // Console and export have separate serial channels. Probe/preflight never
    // start the VM; boot-probe is the explicit VM-starting command.
    let consoleOutput = Pipe()
    let exportOutput = Pipe()
    let console = VZVirtioConsoleDeviceSerialPortConfiguration()
    console.attachment = VZFileHandleSerialPortAttachment(
        fileHandleForReading: nil,
        fileHandleForWriting: consoleOutput.fileHandleForWriting
    )
    let export = VZVirtioConsoleDeviceSerialPortConfiguration()
    export.attachment = VZFileHandleSerialPortAttachment(
        fileHandleForReading: nil,
        fileHandleForWriting: exportOutput.fileHandleForWriting
    )
    configuration.serialPorts = [console, export]

    configuration.networkDevices = []
    configuration.socketDevices = []
    configuration.directorySharingDevices = []
    configuration.graphicsDevices = []
    configuration.audioDevices = []

    guard configuration.networkDevices.isEmpty,
          configuration.socketDevices.isEmpty,
          configuration.directorySharingDevices.isEmpty,
          configuration.storageDevices.count == 1,
          configuration.serialPorts.count == 2,
          diskAttachment.isReadOnly else {
        throw ProbeFailure.unexpectedConfiguration
    }
    try configuration.validate()

    return PreparedConfiguration(
        configuration: configuration,
        diskAttachment: diskAttachment,
        consoleOutput: consoleOutput,
        exportOutput: exportOutput
    )
}

private func probe(kernelURL: URL, initrdURL: URL, diskURL: URL, transactionHex: String? = nil, ext4Fixture: Bool = false) throws -> ProbeEvidence {
    let prepared = try prepareConfiguration(kernelURL: kernelURL, initrdURL: initrdURL, diskURL: diskURL, transactionHex: transactionHex, ext4Fixture: ext4Fixture)
    let virtualMachine = VZVirtualMachine(configuration: prepared.configuration)
    guard virtualMachine.networkDevices.isEmpty, virtualMachine.state == .stopped else {
        throw ProbeFailure.unexpectedConfiguration
    }

    return ProbeEvidence(
        validated: true,
        networkDevices: prepared.configuration.networkDevices.count,
        runtimeNetworkDevices: virtualMachine.networkDevices.count,
        storageDevices: prepared.configuration.storageDevices.count,
        storageReadOnly: prepared.diskAttachment.isReadOnly,
        serialPorts: prepared.configuration.serialPorts.count,
        socketDevices: prepared.configuration.socketDevices.count,
        sharedDirectoryDevices: prepared.configuration.directorySharingDevices.count,
        vmState: "stopped"
    )
}

do {
    if CommandLine.arguments.count == 5, CommandLine.arguments[1] == "probe" {
        let evidence = try probe(
            kernelURL: URL(fileURLWithPath: CommandLine.arguments[2]),
            initrdURL: URL(fileURLWithPath: CommandLine.arguments[3]),
            diskURL: URL(fileURLWithPath: CommandLine.arguments[4])
        )
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        let output = try encoder.encode(evidence)
        FileHandle.standardOutput.write(output)
        FileHandle.standardOutput.write(Data([0x0a]))
    } else if CommandLine.arguments.count == 6, ["preflight", "preflight-ext4"].contains(CommandLine.arguments[1]) {
        let evidence = try probe(
            kernelURL: URL(fileURLWithPath: CommandLine.arguments[2]),
            initrdURL: URL(fileURLWithPath: CommandLine.arguments[3]),
            diskURL: URL(fileURLWithPath: CommandLine.arguments[4]),
            transactionHex: CommandLine.arguments[5],
            ext4Fixture: CommandLine.arguments[1] == "preflight-ext4"
        )
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        let output = try encoder.encode(evidence)
        FileHandle.standardOutput.write(output)
        FileHandle.standardOutput.write(Data([0x0a]))
    } else if CommandLine.arguments.count == 6, ["boot-probe", "boot-ext4"].contains(CommandLine.arguments[1]) {
        let prepared = try prepareConfiguration(
            kernelURL: URL(fileURLWithPath: CommandLine.arguments[2]),
            initrdURL: URL(fileURLWithPath: CommandLine.arguments[3]),
            diskURL: URL(fileURLWithPath: CommandLine.arguments[4]),
            transactionHex: CommandLine.arguments[5],
            ext4Fixture: CommandLine.arguments[1] == "boot-ext4"
        )
        try bootProof(prepared)
    } else {
        throw ProbeFailure.usage
    }
} catch {
    fputs("alpha-inspector probe: \(error)\n", stderr)
    exit(1)
}
