import Foundation
import Virtualization
import Darwin

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
    case invalidExportIdentity

    var description: String {
        switch self {
        case .usage:
            return "usage: alpha-inspector probe <kernel> <initrd> <synthetic.raw> | preflight|boot-probe|preflight-ext4|boot-ext4|preflight-copy|boot-copy <kernel> <initrd> <synthetic.raw> <transaction-hex> | preflight-export|boot-export <kernel> <initrd> <snapshot.raw> <transaction-hex> <device> <inode> <bytes>"
        case .unsafeDisk:
            return "probe accepts only the fixed private synthetic disk or an exact private journaled export snapshot"
        case .unexpectedConfiguration:
            return "Virtualization configuration is not the fixed no-NIC probe configuration"
        case .invalidTransaction:
            return "boot probe requires one nonzero lowercase 16-byte transaction hex value"
        case .invalidExportIdentity:
            return "export requires exact positive decimal snapshot device, inode, and bounded size"
        }
    }
}

struct ExpectedExportDisk {
    let device: UInt64
    let inode: UInt64
    let bytes: Int64
    let transactionID: String
}

private func parseExportDisk(_ arguments: [String], transactionHex: String) throws -> ExpectedExportDisk {
    guard arguments.count == 3,
          transactionHex.count == 32,
          transactionHex != String(repeating: "0", count: 32),
          transactionHex.utf8.allSatisfy({ ($0 >= 48 && $0 <= 57) || ($0 >= 97 && $0 <= 102) }),
          arguments.allSatisfy({ !$0.isEmpty && $0.utf8.allSatisfy({ $0 >= 48 && $0 <= 57 }) }),
          let device = UInt64(arguments[0]), device > 0,
          let inode = UInt64(arguments[1]), inode > 0,
          let bytes = Int64(arguments[2]), bytes >= 4096, bytes <= 1 << 30,
          bytes % 512 == 0 else {
        throw ProbeFailure.invalidExportIdentity
    }
    let start = transactionHex.startIndex
    let pieces = [8, 4, 4, 4, 12]
    var cursor = start
    let dashed = pieces.map { length -> String in
        let end = transactionHex.index(cursor, offsetBy: length)
        defer { cursor = end }
        return String(transactionHex[cursor..<end])
    }.joined(separator: "-")
    return ExpectedExportDisk(device: device, inode: inode, bytes: bytes, transactionID: dashed)
}

// A secondary admission check for the already journaled private snapshot.
// The control plane must also pin and hash it before and after this helper.
func requireExportSnapshotDisk(_ url: URL, expected: ExpectedExportDisk) throws {
    let path = url.path
    let components = path.components(separatedBy: "/")
    let name = components.count >= 3 ? components[components.count - 2] : ""
    guard path.hasPrefix("/"), components.count >= 5,
          components.dropFirst().allSatisfy({ !$0.isEmpty && $0 != "." && $0 != ".." }),
          components.suffix(3).first == "exports", components.last == "snapshot.raw",
          name == expected.transactionID else {
        throw ProbeFailure.unsafeDisk
    }
    let parent = url.deletingLastPathComponent().path
    let exports = url.deletingLastPathComponent().deletingLastPathComponent().path
    var parentInfo = stat()
    var exportsInfo = stat()
    var pathInfo = stat()
    guard lstat(exports, &exportsInfo) == 0, lstat(parent, &parentInfo) == 0,
          lstat(path, &pathInfo) == 0,
          exportsInfo.st_mode & S_IFMT == S_IFDIR,
          parentInfo.st_mode & S_IFMT == S_IFDIR,
          pathInfo.st_mode & S_IFMT == S_IFREG,
          exportsInfo.st_uid == getuid(), parentInfo.st_uid == getuid(),
          pathInfo.st_uid == getuid(),
          exportsInfo.st_mode & 0o7777 == 0o700,
          parentInfo.st_mode & 0o7777 == 0o700,
          pathInfo.st_mode & 0o7777 == 0o600,
          pathInfo.st_nlink == 1,
          UInt64(pathInfo.st_dev) == expected.device,
          UInt64(pathInfo.st_ino) == expected.inode,
          pathInfo.st_size == expected.bytes else {
        throw ProbeFailure.unsafeDisk
    }
    let descriptor = open(path, O_RDONLY | O_NOFOLLOW | O_CLOEXEC)
    guard descriptor >= 0 else { throw ProbeFailure.unsafeDisk }
    defer { _ = close(descriptor) }
    var opened = stat()
    var after = stat()
    guard fstat(descriptor, &opened) == 0, lstat(path, &after) == 0,
          opened.st_dev == pathInfo.st_dev, opened.st_ino == pathInfo.st_ino,
          after.st_dev == pathInfo.st_dev, after.st_ino == pathInfo.st_ino,
          opened.st_size == expected.bytes, after.st_size == expected.bytes,
          opened.st_mode == pathInfo.st_mode, after.st_mode == pathInfo.st_mode,
          opened.st_nlink == 1, after.st_nlink == 1 else {
        throw ProbeFailure.unsafeDisk
    }
}

private func requireSyntheticDisk(_ url: URL, copyFixture: Bool) throws {
    if copyFixture {
        // standardizedFileURL rewrites /private/tmp to its /tmp symlink alias.
        // Check the supplied lexical path, then lstat those exact components.
        let path = url.path
        guard path.range(
            of: #"^/private/tmp/boxwarden-inspector-boot\.[A-Za-z0-9]{6}/synthetic\.raw$"#,
            options: .regularExpression
        ) != nil else {
            throw ProbeFailure.unsafeDisk
        }
        let parent = String(path.dropLast("/synthetic.raw".count))
        var directory = stat()
        var disk = stat()
        guard lstat(parent, &directory) == 0,
              lstat(path, &disk) == 0,
              directory.st_mode & S_IFMT == S_IFDIR,
              disk.st_mode & S_IFMT == S_IFREG,
              directory.st_uid == getuid(), disk.st_uid == getuid(),
              directory.st_mode & 0o077 == 0,
              disk.st_mode & 0o077 == 0,
              disk.st_nlink == 1 else {
            throw ProbeFailure.unsafeDisk
        }
        return
    }
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
    let exportDisk: ExpectedExportDisk?
    let diskURL: URL
}

func prepareConfiguration(kernelURL: URL, initrdURL: URL, diskURL: URL, transactionHex: String?, ext4Fixture: Bool = false, copyFixture: Bool = false, exportDisk: ExpectedExportDisk? = nil) throws -> PreparedConfiguration {
    guard [ext4Fixture, copyFixture, exportDisk != nil].filter({ $0 }).count <= 1 else { throw ProbeFailure.unexpectedConfiguration }
    if let exportDisk {
        try requireExportSnapshotDisk(diskURL, expected: exportDisk)
    } else {
        try requireSyntheticDisk(diskURL, copyFixture: copyFixture)
    }
    let diskValues = try diskURL.resourceValues(forKeys: [.isRegularFileKey, .isSymbolicLinkKey, .fileSizeKey])
    guard diskValues.isRegularFile == true,
          diskValues.isSymbolicLink != true,
          diskValues.fileSize == Int(exportDisk?.bytes ?? Int64(((ext4Fixture || copyFixture) ? 64 : 8) * 1024 * 1024)) else {
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
        let selector = exportDisk != nil ? " alpha_fixture=export" : (ext4Fixture ? " alpha_fixture=ext4" : (copyFixture ? " alpha_fixture=copy" : ""))
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
        exportOutput: exportOutput,
        exportDisk: exportDisk,
        diskURL: diskURL
    )
}

private func probe(kernelURL: URL, initrdURL: URL, diskURL: URL, transactionHex: String? = nil, ext4Fixture: Bool = false, copyFixture: Bool = false, exportDisk: ExpectedExportDisk? = nil) throws -> ProbeEvidence {
    let prepared = try prepareConfiguration(kernelURL: kernelURL, initrdURL: initrdURL, diskURL: diskURL, transactionHex: transactionHex, ext4Fixture: ext4Fixture, copyFixture: copyFixture, exportDisk: exportDisk)
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
    } else if CommandLine.arguments.count == 6, ["preflight", "preflight-ext4", "preflight-copy"].contains(CommandLine.arguments[1]) {
        let evidence = try probe(
            kernelURL: URL(fileURLWithPath: CommandLine.arguments[2]),
            initrdURL: URL(fileURLWithPath: CommandLine.arguments[3]),
            diskURL: URL(fileURLWithPath: CommandLine.arguments[4]),
            transactionHex: CommandLine.arguments[5],
            ext4Fixture: CommandLine.arguments[1] == "preflight-ext4",
            copyFixture: CommandLine.arguments[1] == "preflight-copy"
        )
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        let output = try encoder.encode(evidence)
        FileHandle.standardOutput.write(output)
        FileHandle.standardOutput.write(Data([0x0a]))
    } else if CommandLine.arguments.count == 6, ["boot-probe", "boot-ext4", "boot-copy"].contains(CommandLine.arguments[1]) {
        let prepared = try prepareConfiguration(
            kernelURL: URL(fileURLWithPath: CommandLine.arguments[2]),
            initrdURL: URL(fileURLWithPath: CommandLine.arguments[3]),
            diskURL: URL(fileURLWithPath: CommandLine.arguments[4]),
            transactionHex: CommandLine.arguments[5],
            ext4Fixture: CommandLine.arguments[1] == "boot-ext4",
            copyFixture: CommandLine.arguments[1] == "boot-copy"
        )
        try bootProof(prepared)
    } else if CommandLine.arguments.count == 9, ["preflight-export", "boot-export"].contains(CommandLine.arguments[1]) {
        let exportDisk = try parseExportDisk(Array(CommandLine.arguments[6...8]), transactionHex: CommandLine.arguments[5])
        let kernelURL = URL(fileURLWithPath: CommandLine.arguments[2])
        let initrdURL = URL(fileURLWithPath: CommandLine.arguments[3])
        let diskURL = URL(fileURLWithPath: CommandLine.arguments[4])
        if CommandLine.arguments[1] == "preflight-export" {
            let evidence = try probe(kernelURL: kernelURL, initrdURL: initrdURL,
                                     diskURL: diskURL, transactionHex: CommandLine.arguments[5], exportDisk: exportDisk)
            let encoder = JSONEncoder()
            encoder.outputFormatting = [.sortedKeys]
            FileHandle.standardOutput.write(try encoder.encode(evidence))
            FileHandle.standardOutput.write(Data([0x0a]))
        } else {
            let prepared = try prepareConfiguration(kernelURL: kernelURL, initrdURL: initrdURL,
                                                     diskURL: diskURL, transactionHex: CommandLine.arguments[5], exportDisk: exportDisk)
            try bootProof(prepared)
        }
    } else {
        throw ProbeFailure.usage
    }
} catch {
    fputs("alpha-inspector probe: \(error)\n", stderr)
    exit(1)
}
