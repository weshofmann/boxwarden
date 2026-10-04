import Foundation
import Darwin

enum ProjectActivityStatus: String, Codable { case running, succeeded, failed, unknown }
struct ProjectActivity: Identifiable {
  let id: UUID; let directory: URL; let operation: String; let projectName: String?
  let startedAt: Date; let status: ProjectActivityStatus; let message: String
  var acknowledgedAt: Date? = nil
}

struct ProjectActivityRecord: Codable {
  let id: UUID; let executable: String; let config: String; let domain: String
  let operation: String; let projectName: String?; let arguments: [String]; let mutation: Bool; let startedAt: Date
  var pid: Int32?; var startSeconds: UInt64?; var startMicroseconds: UInt64?
  var status: ProjectActivityStatus; var message: String; var exitStatus: Int32?
  var acknowledgedAt: Date? = nil
  func activity(root: URL) -> ProjectActivity {
    ProjectActivity(id: id, directory: root.appendingPathComponent(id.uuidString), operation: operation,
                    projectName: projectName, startedAt: startedAt, status: status, message: message, acknowledgedAt: acknowledgedAt)
  }
  var processIsAlive: Bool {
    guard let pid, let seconds = startSeconds, let micros = startMicroseconds,
          let identity = Self.identity(pid) else { return false }
    return identity.0 == seconds && identity.1 == micros
  }
  static func identity(_ pid: Int32) -> (UInt64, UInt64)? {
    var info = proc_bsdinfo()
    guard proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &info, Int32(MemoryLayout<proc_bsdinfo>.size)) == MemoryLayout<proc_bsdinfo>.size,
          info.pbi_status != UInt32(SZOMB) else { return nil }
    return (info.pbi_start_tvsec, info.pbi_start_tvusec)
  }
}

// Open every path component with O_NOFOLLOW, then operate relative to retained
// descriptors. No receipt path, output file, or metadata file follows symlinks.
final class ProjectActivityStore {
  let root: URL
  private let rootFD: Int32
  private let lockFD: Int32
  private let lock = NSLock()
  static let maximumOutput = 8 * 1024 * 1024
  static let maximumRecord = 128 * 1024
  init(root: URL) throws {
    guard ProjectCommand.validPath(root.path), root.path != "/" else { throw ProjectClientError.activity("Invalid activity directory") }
    self.root = root
    var fd = Darwin.open("/", O_RDONLY | O_DIRECTORY | O_CLOEXEC)
    guard fd >= 0 else { throw ProjectClientError.activity("Cannot open activity ancestry") }
    guard Self.hasNoGrantACL(fd) else { Darwin.close(fd); throw ProjectClientError.activity("Activity ancestry has granting or unverifiable ACLs") }
    do {
      let parts = root.path.split(separator: "/").map(String.init)
      for (index, part) in parts.enumerated() {
        if mkdirat(fd, part, 0o700) != 0 && errno != EEXIST { throw ProjectClientError.activity("Cannot create private activity directory") }
        let next = openat(fd, part, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC)
        guard next >= 0 else { throw ProjectClientError.activity("Activity ancestry must contain ordinary directories") }
        var st = stat()
        let valid = fstat(next, &st) == 0 && (st.st_uid == getuid() || st.st_uid == 0) &&
          ((st.st_mode & 0o022) == 0 || (st.st_uid == 0 && (st.st_mode & mode_t(S_ISVTX)) != 0)) &&
          (index != parts.count - 1 || (st.st_uid == getuid() && (st.st_mode & 0o077) == 0)) && Self.hasNoGrantACL(next)
        Darwin.close(fd); fd = next
        guard valid else { throw ProjectClientError.activity("Activity directory ownership or permissions are unsafe") }
      }
      rootFD = fd
      let openedLock = openat(fd, ".lock", O_RDWR | O_CREAT | O_NOFOLLOW | O_CLOEXEC, 0o600)
      guard openedLock >= 0 else { throw ProjectClientError.activity("Cannot open activity lock") }
      guard Self.privateFile(openedLock) else { Darwin.close(openedLock); throw ProjectClientError.activity("Unsafe activity lock") }
      lockFD = openedLock
    } catch { Darwin.close(fd); throw error }
  }
  deinit { Darwin.close(lockFD); Darwin.close(rootFD) }
  func withLock<T>(_ body: () throws -> T) throws -> T {
    guard lock.lock(before: Date().addingTimeInterval(0.1)) else {
      throw ProjectClientError.busy("Command activity is busy; refresh before retry")
    }
    defer { lock.unlock() }
    guard Self.privateDirectory(rootFD), Self.privateFile(lockFD) else { throw ProjectClientError.activity("Activity root or lock permissions/ACLs are unsafe") }
    guard flock(lockFD, LOCK_EX | LOCK_NB) == 0 else {
      if errno == EWOULDBLOCK || errno == EAGAIN { throw ProjectClientError.busy("Command activity is busy in another frontend; refresh before retry") }
      throw ProjectClientError.activity("Cannot lock command activity")
    }
    defer { _ = flock(lockFD, LOCK_UN) }
    return try body()
  }
  func directory(_ id: UUID) throws -> Int32 {
    let fd = openat(rootFD, id.uuidString, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC)
    var st = stat()
    guard fd >= 0 else { throw ProjectClientError.activity("Cannot open command activity") }
    guard fstat(fd, &st) == 0, st.st_uid == getuid(), st.st_mode & 0o077 == 0, Self.hasNoGrantACL(fd) else {
      Darwin.close(fd); throw ProjectClientError.activity("Unsafe command activity directory")
    }
    return fd
  }
  func create(_ record: ProjectActivityRecord) throws -> Int32 {
    // Reject before mkdir: an unreadable receipt must never poison discovery.
    _ = try encodedRecord(record)
    guard mkdirat(rootFD, record.id.uuidString, 0o700) == 0 else { throw ProjectClientError.activity("Cannot create command receipt") }
    try save(record)
    let fd = try directory(record.id); defer { Darwin.close(fd) }
    let output = openat(fd, "events.jsonl", O_RDWR | O_CREAT | O_EXCL | O_APPEND | O_NOFOLLOW | O_CLOEXEC, 0o600)
    guard output >= 0, Self.privateFile(output), fsync(fd) == 0, fsync(rootFD) == 0 else {
      if output >= 0 { Darwin.close(output) }
      throw ProjectClientError.activity("Cannot create durable command output")
    }
    return output
  }
  private func encodedRecord(_ record: ProjectActivityRecord) throws -> Data {
    let data = try JSONEncoder().encode(record)
    guard data.count <= Self.maximumRecord else { throw ProjectClientError.activity("Command receipt exceeds its 128 KiB limit") }
    return data
  }
  func save(_ record: ProjectActivityRecord) throws {
    let data = try encodedRecord(record)
    let fd = try directory(record.id); defer { Darwin.close(fd) }
    let existing = openat(fd, "record.json", O_RDONLY | O_NOFOLLOW | O_CLOEXEC)
    if existing >= 0 {
      let safe = Self.privateFile(existing); Darwin.close(existing)
      guard safe else { throw ProjectClientError.activity("Existing command receipt permissions/ACLs are unsafe") }
    } else if errno != ENOENT { throw ProjectClientError.activity("Cannot inspect existing command receipt") }
    let temporary = ".record-" + UUID().uuidString
    let output = openat(fd, temporary, O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW | O_CLOEXEC, 0o600)
    guard output >= 0 else { throw ProjectClientError.activity("Cannot persist command activity") }
    guard Self.privateFile(output) else { Darwin.close(output); _ = unlinkat(fd, temporary, 0); throw ProjectClientError.activity("Command receipt permissions/ACLs are unsafe") }
    defer { Darwin.close(output); _ = unlinkat(fd, temporary, 0) }
    try data.withUnsafeBytes { bytes in
      var offset = 0
      while offset < bytes.count {
        let count = Darwin.write(output, bytes.baseAddress!.advanced(by: offset), bytes.count - offset)
        if count < 0 && errno == EINTR { continue }
        guard count > 0 else { throw ProjectClientError.activity("Cannot write command receipt") }
        offset += count
      }
    }
    guard fsync(output) == 0, renameat(fd, temporary, fd, "record.json") == 0, fsync(fd) == 0 else {
      throw ProjectClientError.activity("Cannot flush command receipt")
    }
  }
  func records() throws -> [ProjectActivityRecord] {
    let names = try FileManager.default.contentsOfDirectory(atPath: root.path)
    guard names.count <= 4097 else { throw ProjectClientError.activity("Too many retained command activities") }
    return try names.filter { $0 != ".lock" }.map { name in
      guard let id = UUID(uuidString: name), id.uuidString == name else { throw ProjectClientError.activity("Unrecognized command activity entry") }
      let fd = try directory(id); defer { Darwin.close(fd) }
      let record = try JSONDecoder().decode(ProjectActivityRecord.self, from: readFile(fd, "record.json", maximum: Self.maximumRecord))
      guard record.id == id else { throw ProjectClientError.activity("Mismatched command receipt") }
      return record
    }
  }
  // Refresh and preview requests are disposable reads. Retain only a bounded
  // history; mutation receipts remain durable until explicitly managed.
  func pruneQueries() throws {
    let disposable = try records().filter { !$0.mutation && $0.status != .running }
      .sorted { $0.startedAt > $1.startedAt }
    for record in disposable.dropFirst(16) {
      let fd = try directory(record.id); defer { Darwin.close(fd) }
      let names = try FileManager.default.contentsOfDirectory(atPath: root.appendingPathComponent(record.id.uuidString).path)
      guard names.allSatisfy({ $0 == "record.json" || $0 == "events.jsonl" || ($0.hasPrefix(".record-") && UUID(uuidString: String($0.dropFirst(8))) != nil) }) else {
        throw ProjectClientError.activity("Unexpected files in disposable query activity")
      }
      for name in names { guard unlinkat(fd, name, 0) == 0 else { throw ProjectClientError.activity("Cannot prune old query activity") } }
      guard unlinkat(rootFD, record.id.uuidString, AT_REMOVEDIR) == 0 else { throw ProjectClientError.activity("Cannot prune old query directory") }
    }
    if disposable.count > 16 { guard fsync(rootFD) == 0 else { throw ProjectClientError.activity("Cannot flush query retention") } }
  }
  func events(_ id: UUID) throws -> Data {
    let fd = try directory(id); defer { Darwin.close(fd) }
    return try readFile(fd, "events.jsonl", maximum: Self.maximumOutput)
  }
  private func readFile(_ directory: Int32, _ name: String, maximum: Int) throws -> Data {
    let fd = openat(directory, name, O_RDONLY | O_NOFOLLOW | O_CLOEXEC)
    guard fd >= 0 else { throw ProjectClientError.activity("Cannot read command activity") }
    defer { Darwin.close(fd) }
    var st = stat()
    guard Self.privateFile(fd), fstat(fd, &st) == 0, st.st_size <= maximum else { throw ProjectClientError.activity("Command activity is unsafe or exceeds its size bound") }
    var result = Data(); var buffer = [UInt8](repeating: 0, count: 16384)
    while true {
      let count = Darwin.read(fd, &buffer, buffer.count)
      if count < 0 && errno == EINTR { continue }
      guard count >= 0 else { throw ProjectClientError.activity("Cannot read command activity") }
      if count == 0 { return result }
      guard result.count + count <= maximum else { throw ProjectClientError.activity("Command output exceeds its size bound") }
      result.append(contentsOf: buffer.prefix(count))
    }
  }
  private static func privateDirectory(_ fd: Int32) -> Bool {
    var st = stat()
    return fstat(fd, &st) == 0 && (st.st_mode & S_IFMT) == S_IFDIR && st.st_uid == getuid() && st.st_mode & 0o077 == 0 && hasNoGrantACL(fd)
  }
  // Darwin ACL grants can expose 0600 files or be inherited by newly created
  // receipts. Inspect the exact opened inode; deny-only ACLs remain intact.
  private static func hasNoGrantACL(_ fd: Int32) -> Bool {
    guard let acl = acl_get_fd_np(fd, ACL_TYPE_EXTENDED) else { return errno == ENOENT }
    defer { _ = acl_free(UnsafeMutableRawPointer(acl)) }
    guard acl_valid(acl) == 0 else { return false }
    var entry: acl_entry_t?
    var selector = Int32(ACL_FIRST_ENTRY.rawValue)
    for _ in 0..<129 {
      if acl_get_entry(acl, selector, &entry) != 0 { return errno == EINVAL }
      guard let entry else { return false }
      var tag = ACL_UNDEFINED_TAG
      guard acl_get_tag_type(entry, &tag) == 0, tag == ACL_EXTENDED_DENY else { return false }
      selector = Int32(ACL_NEXT_ENTRY.rawValue)
    }
    return false
  }
  static func privateFile(_ fd: Int32) -> Bool {
    var st = stat()
    return fstat(fd, &st) == 0 && (st.st_mode & S_IFMT) == S_IFREG && st.st_uid == getuid() && st.st_nlink == 1 && st.st_mode & 0o077 == 0 && hasNoGrantACL(fd)
  }
}
