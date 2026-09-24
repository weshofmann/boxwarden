import Darwin
import Foundation
import Virtualization

enum FormatterFailure: Error, CustomStringConvertible {
    case usage
    case unsafeDisk(String)
    case invalidRequest
    case unexpectedConfiguration

    var description: String {
        switch self {
        case .usage:
            return "usage: alpha-formatter preflight|preflight-run|run <kernel> <initrd> <managed-raw> <device> <inode> <size> <transaction> <filesystem-uuid> <marker>"
        case .unsafeDisk(let reason):
            return "formatter disk is not the exact private regular one-link raw file: \(reason)"
        case .invalidRequest:
            return "formatter boot request is invalid"
        case .unexpectedConfiguration:
            return "formatter VZ configuration is not the fixed isolated configuration"
        }
    }
}

struct FormatterArguments {
    let command: String
    let kernelURL: URL
    let initrdURL: URL
    let diskURL: URL
    let device: UInt64
    let inode: UInt64
    let size: Int64
    let transaction: String
    let filesystemUUID: String
    let marker: String

    init(_ arguments: [String]) throws {
        guard arguments.count == 11,
              ["preflight", "preflight-run", "run"].contains(arguments[1]),
              let device = UInt64(arguments[5]),
              let inode = UInt64(arguments[6]),
              let size = Int64(arguments[7]),
              size >= 4096, size <= 1 << 43, size % 512 == 0,
              lexicallyCleanAbsolute(arguments[4]),
              Self.lowerHex(arguments[8], length: 32),
              arguments[8] != String(repeating: "0", count: 32),
              Self.uuid(arguments[9]),
              Self.lowerHex(arguments[10], length: 64) else {
            throw FormatterFailure.invalidRequest
        }
        guard arguments[10] != String(repeating: "0", count: 64) else {
            throw FormatterFailure.invalidRequest
        }
        command = arguments[1]
        kernelURL = URL(fileURLWithPath: arguments[2])
        initrdURL = URL(fileURLWithPath: arguments[3])
        diskURL = URL(fileURLWithPath: arguments[4])
        self.device = device
        self.inode = inode
        self.size = size
        transaction = arguments[8]
        filesystemUUID = arguments[9]
        marker = arguments[10]
    }

    static func lowerHex(_ value: String, length: Int) -> Bool {
        value.utf8.count == length && value.utf8.allSatisfy { ($0 >= 48 && $0 <= 57) || ($0 >= 97 && $0 <= 102) }
    }

    static func uuid(_ value: String) -> Bool {
        let bytes = Array(value.utf8)
        guard bytes.count == 36 else { return false }
        for index in 0..<36 {
            if [8, 13, 18, 23].contains(index) {
                if bytes[index] != 45 { return false }
            } else if !((bytes[index] >= 48 && bytes[index] <= 57) || (bytes[index] >= 97 && bytes[index] <= 102)) {
                return false
            }
        }
        return true
    }
}

private func metadata(_ path: String) throws -> stat {
    var result = stat()
    guard lstat(path, &result) == 0 else { throw FormatterFailure.unsafeDisk("lstat failed") }
    return result
}

private func sameIdentity(_ value: stat, _ arguments: FormatterArguments) -> Bool {
    UInt64(value.st_dev) == arguments.device && value.st_ino == arguments.inode && value.st_size == arguments.size
}

private func lexicallyCleanAbsolute(_ path: String) -> Bool {
    let parts = path.split(separator: "/", omittingEmptySubsequences: false)
    return parts.count > 2 && parts[0].isEmpty && parts.dropFirst().allSatisfy { !$0.isEmpty && $0 != "." && $0 != ".." }
}

private func requireNoExtendedACL(_ path: String, _ admitted: stat) throws {
    errno = 0
    let acl = acl_get_file(path, ACL_TYPE_EXTENDED)
    if let acl {
        acl_free(UnsafeMutableRawPointer(acl))
        throw FormatterFailure.unsafeDisk("extended ACL on private path")
    }
    guard errno == ENOENT else {
        throw FormatterFailure.unsafeDisk("private path ACL inspection failed")
    }
    let current = try metadata(path)
    guard current.st_dev == admitted.st_dev, current.st_ino == admitted.st_ino,
          current.st_mode == admitted.st_mode, current.st_uid == admitted.st_uid else {
        throw FormatterFailure.unsafeDisk("private path changed during ACL inspection")
    }
}

private func isFreshSyntheticRoot(_ path: String) -> Bool {
    let prefix = "/private/tmp/boxwarden-alpha-formatter-boot."
    guard path.hasPrefix(prefix) else { return false }
    let suffix = path.dropFirst(prefix.count)
    return suffix.utf8.count == 8 && suffix.utf8.allSatisfy {
        ($0 >= 48 && $0 <= 57) || ($0 >= 65 && $0 <= 90) || ($0 >= 97 && $0 <= 122) || $0 == 95
    }
}

struct PreparedFormatter {
    let configuration: VZVirtualMachineConfiguration
    let diskAttachment: VZDiskImageStorageDeviceAttachment
    let diskHandle: FileHandle
    let consoleOutput: Pipe
    let reportOutput: Pipe
    let arguments: FormatterArguments
    let journalHandle: FileHandle?
    let journalPath: String?
    let journalIdentity: UInt64?
    let journalBytes: Data?

    func recheckDisk() throws {
        let current = try metadata(arguments.diskURL.path)
        var opened = stat()
        guard fstat(diskHandle.fileDescriptor, &opened) == 0,
              sameIdentity(current, arguments), sameIdentity(opened, arguments),
              current.st_mode & mode_t(S_IFMT) == mode_t(S_IFREG),
              current.st_mode & 0o777 == 0o600,
              current.st_nlink == 1, current.st_uid == getuid() else {
            throw FormatterFailure.unsafeDisk("post-open file identity or metadata changed")
        }
        try requireNoExtendedACL(arguments.diskURL.path, current)
    }

    func recheckJournal() throws {
        guard let journalHandle, let journalPath, let journalIdentity, let journalBytes else {
            throw FormatterFailure.unsafeDisk("creating journal is absent")
        }
        let current = try metadata(journalPath)
        var opened = stat()
        guard fstat(journalHandle.fileDescriptor, &opened) == 0,
              current.st_ino == journalIdentity, opened.st_ino == journalIdentity,
              current.st_dev == opened.st_dev, current.st_size == opened.st_size,
              current.st_nlink == 1, current.st_uid == getuid(),
              current.st_mode & mode_t(S_IFMT) == mode_t(S_IFREG),
              current.st_mode & 0o777 == 0o600,
              current.st_size == journalBytes.count else {
            throw FormatterFailure.unsafeDisk("creating journal identity or metadata changed")
        }
        try requireNoExtendedACL(journalPath, current)
        try journalHandle.seek(toOffset: 0)
        guard try journalHandle.readToEnd() == journalBytes else {
            throw FormatterFailure.unsafeDisk("creating journal bytes changed")
        }
    }
}

private struct JournalIdentity: Decodable {
    let device: UInt64
    let inode: UInt64
}

private struct CreatingJournal: Decodable {
    let version: Int
    let domain: String
    let volumeID: String
    let filesystemUUID: String
    let sizeBytes: Int64
    let state: String
    let identity: JournalIdentity?
    let evidence: GuestJournalEvidence?

    enum CodingKeys: String, CodingKey {
        case version, domain, state, identity, evidence
        case volumeID = "volume_id"
        case filesystemUUID = "filesystem_uuid"
        case sizeBytes = "size_bytes"
    }
}

private struct GuestJournalEvidence: Decodable {}

private func admitCreatingJournal(_ arguments: FormatterArguments) throws -> (FileHandle, String, UInt64, Data) {
    let volumeName = arguments.diskURL.deletingPathExtension().lastPathComponent
    guard FormatterArguments.uuid(volumeName) else {
        throw FormatterFailure.unsafeDisk("raw file name is not a volume UUID")
    }
    let journalPath = arguments.diskURL.deletingLastPathComponent().appendingPathComponent(volumeName + ".format.json").path
    let initial = try metadata(journalPath)
    guard initial.st_mode & mode_t(S_IFMT) == mode_t(S_IFREG),
          initial.st_mode & 0o777 == 0o600,
          initial.st_uid == getuid(), initial.st_nlink == 1,
          initial.st_size > 0, initial.st_size <= 4096 else {
        throw FormatterFailure.unsafeDisk("creating journal is not a private bounded regular file")
    }
    try requireNoExtendedACL(journalPath, initial)
    let descriptor = open(journalPath, O_RDONLY | O_NOFOLLOW | O_CLOEXEC)
    guard descriptor >= 0 else { throw FormatterFailure.unsafeDisk("creating journal no-follow open failed") }
    let handle = FileHandle(fileDescriptor: descriptor, closeOnDealloc: true)
    var opened = stat()
    guard fstat(descriptor, &opened) == 0,
          initial.st_dev == opened.st_dev, initial.st_ino == opened.st_ino,
          initial.st_size == opened.st_size else {
        throw FormatterFailure.unsafeDisk("creating journal changed while opening")
    }
    let bytes = try handle.readToEnd() ?? Data()
    guard bytes.count == initial.st_size,
          let journal = try? JSONDecoder().decode(CreatingJournal.self, from: bytes),
          journal.version == 1, !journal.domain.isEmpty,
          journal.volumeID == volumeName,
          journal.filesystemUUID == arguments.filesystemUUID,
          journal.sizeBytes == arguments.size,
          journal.state == "formatting", journal.evidence == nil,
          journal.identity?.device == arguments.device,
          journal.identity?.inode == arguments.inode else {
        throw FormatterFailure.unsafeDisk("creating journal does not bind this exact format")
    }
    return (handle, journalPath, initial.st_ino, bytes)
}

func prepareFormatter(_ arguments: FormatterArguments) throws -> PreparedFormatter {
    let path = arguments.diskURL.path
    guard lexicallyCleanAbsolute(path) else {
        throw FormatterFailure.unsafeDisk("path is not lexically clean and absolute")
    }
    guard arguments.diskURL.deletingLastPathComponent().lastPathComponent == "volumes" else {
        throw FormatterFailure.unsafeDisk("parent is not volumes")
    }
    guard arguments.diskURL.lastPathComponent.hasSuffix(".raw") else {
        throw FormatterFailure.unsafeDisk("filename is not raw")
    }
    let rootPath = arguments.diskURL.deletingLastPathComponent().deletingLastPathComponent().path
    if arguments.command != "preflight" && !isFreshSyntheticRoot(rootPath) {
        throw FormatterFailure.unsafeDisk("run target is outside a fresh synthetic probe root")
    }
    let parent = try metadata(arguments.diskURL.deletingLastPathComponent().path)
    let root = try metadata(rootPath)
    guard parent.st_mode & mode_t(S_IFMT) == mode_t(S_IFDIR),
          parent.st_mode & 0o777 == 0o700, parent.st_uid == getuid(),
          root.st_mode & mode_t(S_IFMT) == mode_t(S_IFDIR),
          root.st_mode & 0o777 == 0o700, root.st_uid == getuid() else {
        throw FormatterFailure.unsafeDisk("volume directory metadata differs")
    }
    try requireNoExtendedACL(rootPath, root)
    try requireNoExtendedACL(arguments.diskURL.deletingLastPathComponent().path, parent)
    let initial = try metadata(path)
    guard sameIdentity(initial, arguments),
          initial.st_mode & mode_t(S_IFMT) == mode_t(S_IFREG),
          initial.st_mode & 0o777 == 0o600,
          initial.st_nlink == 1, initial.st_uid == getuid() else {
        throw FormatterFailure.unsafeDisk("initial file identity or metadata differs")
    }
    try requireNoExtendedACL(path, initial)
    let descriptor = open(path, O_RDWR | O_NOFOLLOW | O_CLOEXEC)
    guard descriptor >= 0 else { throw FormatterFailure.unsafeDisk("no-follow open failed") }
    let handle = FileHandle(fileDescriptor: descriptor, closeOnDealloc: true)
    var markerPrefix = [UInt8](repeating: 0, count: 32)
    guard pread(descriptor, &markerPrefix, markerPrefix.count, 0) == markerPrefix.count,
          markerPrefix.map({ String(format: "%02x", $0) }).joined() == arguments.marker else {
        throw FormatterFailure.unsafeDisk("raw file marker differs")
    }
    let journal = arguments.command == "preflight" ? nil : try admitCreatingJournal(arguments)

    let configuration = VZVirtualMachineConfiguration()
    configuration.cpuCount = 2
    configuration.memorySize = 2 * 1024 * 1024 * 1024
    configuration.platform = VZGenericPlatformConfiguration()
    let loader = VZLinuxBootLoader(kernelURL: arguments.kernelURL)
    loader.initialRamdiskURL = arguments.initrdURL
    loader.commandLine = "console=hvc0 rdinit=/alpha-formatter alpha_tx=\(arguments.transaction) alpha_uuid=\(arguments.filesystemUUID) alpha_size=\(arguments.size) alpha_marker=\(arguments.marker)"
    configuration.bootLoader = loader

    let attachment = try VZDiskImageStorageDeviceAttachment(url: arguments.diskURL, readOnly: false)
    configuration.storageDevices = [VZVirtioBlockDeviceConfiguration(attachment: attachment)]
    let consoleOutput = Pipe()
    let reportOutput = Pipe()
    let console = VZVirtioConsoleDeviceSerialPortConfiguration()
    console.attachment = VZFileHandleSerialPortAttachment(fileHandleForReading: nil, fileHandleForWriting: consoleOutput.fileHandleForWriting)
    let report = VZVirtioConsoleDeviceSerialPortConfiguration()
    report.attachment = VZFileHandleSerialPortAttachment(fileHandleForReading: nil, fileHandleForWriting: reportOutput.fileHandleForWriting)
    configuration.serialPorts = [console, report]
    configuration.networkDevices = []
    configuration.socketDevices = []
    configuration.directorySharingDevices = []
    configuration.graphicsDevices = []
    configuration.audioDevices = []
    guard configuration.networkDevices.isEmpty,
          configuration.socketDevices.isEmpty,
          configuration.directorySharingDevices.isEmpty,
          configuration.graphicsDevices.isEmpty,
          configuration.audioDevices.isEmpty,
          configuration.storageDevices.count == 1,
          configuration.serialPorts.count == 2,
          !attachment.isReadOnly else {
        throw FormatterFailure.unexpectedConfiguration
    }
    try configuration.validate()
    let prepared = PreparedFormatter(configuration: configuration, diskAttachment: attachment, diskHandle: handle, consoleOutput: consoleOutput, reportOutput: reportOutput, arguments: arguments, journalHandle: journal?.0, journalPath: journal?.1, journalIdentity: journal?.2, journalBytes: journal?.3)
    try prepared.recheckDisk()
    if arguments.command != "preflight" { try prepared.recheckJournal() }
    return prepared
}

private struct PreflightEvidence: Encodable {
    let validated = true
    let networkDevices = 0
    let storageDevices = 1
    let storageReadOnly = false
    let serialPorts = 2
    let socketDevices = 0
    let sharedDirectoryDevices = 0
    let vmState = "stopped"

    enum CodingKeys: String, CodingKey {
        case validated
        case networkDevices = "network_devices"
        case storageDevices = "storage_devices"
        case storageReadOnly = "storage_read_only"
        case serialPorts = "serial_ports"
        case socketDevices = "socket_devices"
        case sharedDirectoryDevices = "shared_directory_devices"
        case vmState = "vm_state"
    }
}

do {
    let arguments = try FormatterArguments(CommandLine.arguments)
    let prepared = try prepareFormatter(arguments)
    if arguments.command != "run" {
        let vm = VZVirtualMachine(configuration: prepared.configuration)
        guard vm.state == .stopped, vm.networkDevices.isEmpty else {
            throw FormatterFailure.unexpectedConfiguration
        }
        try prepared.recheckDisk()
        let result = try JSONEncoder().encode(PreflightEvidence())
        FileHandle.standardOutput.write(result)
        FileHandle.standardOutput.write(Data([0x0a]))
    } else {
        try runFormatterVM(prepared)
    }
} catch {
    fputs("alpha-formatter: \(error)\n", stderr)
    exit(1)
}
