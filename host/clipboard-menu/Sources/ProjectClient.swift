import Foundation
import Darwin

final class ProjectClient {
  let executable: String; let config: String; let domain: String
  private let store: ProjectActivityStore
  private let lock = NSLock()
  private var owned: [UUID: ProjectOwnedProcess] = [:]
  init(executable: String, config: String, domain: String = "alpha", activityDirectory: URL? = nil) throws {
    guard ProjectCommand.validPath(executable), ProjectCommand.validPath(config), ProjectCommand.validToken(domain) else {
      throw ProjectClientError.invalidRequest("Invalid CLI, configuration, or domain path")
    }
    self.executable = executable; self.config = config; self.domain = domain
    let directory = activityDirectory ?? FileManager.default.homeDirectoryForCurrentUser
      .appendingPathComponent("Library/Application Support/org.boxwarden.project-manager/CommandActivity")
    store = try ProjectActivityStore(root: directory)
  }
  // Intended for a worker queue. Mutation lifetimes are never bounded by a UI timeout.
  func query(_ command: ProjectCommand, timeout: TimeInterval = 30) throws -> ProjectResponse {
    guard !command.isMutation else { throw ProjectClientError.invalidRequest("Use asynchronous start for mutations") }
    let done = DispatchSemaphore(value: 0)
    let resultLock = NSLock()
    var result: ProjectOutcome?
    let activity = try start(command, progress: { _ in }) { outcome in
      resultLock.lock(); result = outcome; resultLock.unlock(); done.signal()
    }
    if done.wait(timeout: .now() + timeout) != .success {
      lock.lock(); let child = owned[activity.id]; lock.unlock()
      child?.interrupt()
      throw ProjectClientError.process("Query timed out; refresh to rediscover backend state")
    }
    resultLock.lock(); let outcome = result; resultLock.unlock()
    switch outcome {
    case .success(let response): return response
    case .failed(let message), .unknown(let message): throw ProjectClientError.process(message)
    case nil: throw ProjectClientError.process("Query outcome unavailable")
    }
  }
  @discardableResult
  func start(_ command: ProjectCommand, allowRetryAfterUnknown: Bool = false,
             progress: @escaping (ProjectEvent) -> Void = { _ in },
             completion: @escaping (ProjectOutcome) -> Void) throws -> ProjectActivity {
    let arguments = try command.arguments(config: config, domain: domain)
    var record = ProjectActivityRecord(id: UUID(), executable: executable, config: config, domain: domain,
      operation: command.operation, projectName: command.projectName, arguments: arguments,
      mutation: command.isMutation, startedAt: Date(), status: .running, message: "Starting CLI request")
    let child: ProjectOwnedProcess = try store.withLock {
      try store.pruneQueries()
      if command.isMutation {
        let prior = try store.records().filter { $0.mutation && $0.config == config && $0.domain == domain && $0.projectName == command.projectName }
        // Check all live conflicts before recording any explicit acknowledgement.
        guard !prior.contains(where: { ($0.status == .running || $0.status == .unknown) && $0.processIsAlive }) else {
          throw ProjectClientError.busy("A CLI request for this project is still running; inspect retained activity and refresh")
        }
        let unknown = prior.filter { ($0.status == .running || $0.status == .unknown) && $0.acknowledgedAt == nil }
        guard unknown.isEmpty || allowRetryAfterUnknown else {
          throw ProjectClientError.busy("A prior request has an unknown outcome; refresh and explicitly retry through backend admission")
        }
        if allowRetryAfterUnknown {
          for var historical in unknown {
            historical.status = .unknown
            historical.acknowledgedAt = Date()
            try store.save(historical)
          }
        }
      }
      let output = try store.create(record)
      do {
        let pid = try ProjectOwnedProcess.spawn(executable: executable, arguments: arguments, output: output)
        record.pid = pid
        if let identity = ProjectActivityRecord.identity(pid) { record.startSeconds = identity.0; record.startMicroseconds = identity.1 }
        record.message = "CLI request running"
        // A crash between spawn and this write leaves an unknown receipt, which
        // still blocks replay. A remembered PID is never used for signalling.
        try? store.save(record)
        return ProjectOwnedProcess(record: record, output: output, store: store, progress: progress) { [self] outcome in
          lock.lock(); owned.removeValue(forKey: record.id); lock.unlock()
          completion(outcome)
        }
      } catch {
        Darwin.close(output); record.status = .failed; record.message = error.localizedDescription
        try store.save(record); throw error
      }
    }
    lock.lock(); owned[record.id] = child; lock.unlock()
    child.begin()
    return record.activity(root: store.root)
  }
  func acknowledgeUnknownActivity(_ id: UUID) throws {
    try store.withLock {
      guard var record = try store.records().first(where: { $0.id == id }),
            record.config == config, record.domain == domain, record.mutation,
            record.status == .unknown, !record.processIsAlive else {
        throw ProjectClientError.activity("Only an ended unknown request in this configuration and domain can be acknowledged")
      }
      if record.acknowledgedAt == nil { record.acknowledgedAt = Date(); try store.save(record) }
    }
  }
  func recoverActivities() throws -> [ProjectActivity] {
    try store.withLock {
      try store.records().filter { $0.config == config && $0.domain == domain }.map { prior in
        var record = prior
        if record.status == .running && !record.processIsAlive {
          record.status = .unknown; record.message = "CLI lifetime ended without a retained exit receipt; refresh before explicit retry"
          try store.save(record)
        }
        return record.activity(root: store.root)
      }.sorted { $0.startedAt > $1.startedAt }
    }
  }
  func activityEvents(_ id: UUID) throws -> [ProjectEvent] {
    try store.withLock {
      guard let record = try store.records().first(where: { $0.id == id }) else { throw ProjectClientError.activity("Unknown activity") }
      let data = try store.events(id)
      return try data.split(separator: 10, omittingEmptySubsequences: false).dropLast().map {
        try ProjectEvent.decode(Data($0), operation: record.operation)
      }
    }
  }
  // Only an explicitly requested interruption of an export owned by this live
  // instance is allowed. Recovered PIDs cannot enter this owner collection.
  @discardableResult func cancelActiveActivity(_ id: UUID) -> Bool {
    lock.lock(); let child = owned[id]; lock.unlock()
    guard let child, ["project.export", "project.export.retry"].contains(child.operation) else { return false }
    child.interrupt(); return true
  }
}

private final class ProjectOwnedProcess: @unchecked Sendable {
  let operation: String
  private let queue = DispatchQueue(label: "boxwarden.project-client.child")
  private var record: ProjectActivityRecord
  private let output: Int32; private let store: ProjectActivityStore
  private var pid: pid_t; private var total = 0; private var pending = Data(); private var scannedBytes = 0
  private var terminal: ProjectEvent?; private var invalid: String?; private var interrupted = false
  private var reaped = false; private var finished = false
  private let progress: (ProjectEvent) -> Void; private let completion: (ProjectOutcome) -> Void
  init(record: ProjectActivityRecord, output: Int32, store: ProjectActivityStore,
       progress: @escaping (ProjectEvent) -> Void, completion: @escaping (ProjectOutcome) -> Void) {
    self.record = record; self.operation = record.operation; self.output = output; self.store = store
    self.pid = record.pid!; self.progress = progress; self.completion = completion
  }
  static func spawn(executable: String, arguments: [String], output: Int32) throws -> pid_t {
    var actions: posix_spawn_file_actions_t?; var attributes: posix_spawnattr_t?
    guard posix_spawn_file_actions_init(&actions) == 0 else { throw ProjectClientError.process("Cannot initialize CLI launch") }
    defer { posix_spawn_file_actions_destroy(&actions) }
    guard posix_spawnattr_init(&attributes) == 0 else { throw ProjectClientError.process("Cannot initialize CLI attributes") }
    defer { posix_spawnattr_destroy(&attributes) }
    guard posix_spawnattr_setflags(&attributes, Int16(POSIX_SPAWN_CLOEXEC_DEFAULT | POSIX_SPAWN_SETSID)) == 0,
          posix_spawn_file_actions_addopen(&actions, STDIN_FILENO, "/dev/null", O_RDONLY, 0) == 0,
          posix_spawn_file_actions_adddup2(&actions, output, STDOUT_FILENO) == 0,
          posix_spawn_file_actions_addopen(&actions, STDERR_FILENO, "/dev/null", O_WRONLY, 0) == 0 else {
      throw ProjectClientError.process("Cannot isolate CLI descriptors")
    }
    let argv = ([executable] + arguments).map { strdup($0) } + [nil]
    let environmentStrings: [String] = ["PATH=/usr/bin:/bin", "LANG=en_US.UTF-8", "HOME=" + FileManager.default.homeDirectoryForCurrentUser.path]
    let environment = environmentStrings.map { strdup($0) } + [nil]
    defer { argv.forEach { free($0) }; environment.forEach { free($0) } }
    var pid: pid_t = 0
    let code = argv.withUnsafeBufferPointer { a in environment.withUnsafeBufferPointer { e in
      posix_spawn(&pid, executable, &actions, &attributes,
                  UnsafeMutablePointer(mutating: a.baseAddress!), UnsafeMutablePointer(mutating: e.baseAddress!))
    } }
    guard code == 0 else { throw ProjectClientError.process("Cannot launch CLI: " + String(cString: strerror(code))) }
    return pid
  }
  func begin() { queue.async { [self] in poll() } }
  func interrupt() {
    queue.async { [self] in
      guard !reaped, !finished else { return }
      interrupted = true
      _ = kill(pid, SIGTERM)
      queue.asyncAfter(deadline: .now() + .seconds(1)) { [self] in
        guard !reaped, !finished else { return }; _ = kill(pid, SIGKILL)
      }
    }
  }
  private func readAvailable() {
    var st = stat()
    guard ProjectActivityStore.privateFile(output), fstat(output, &st) == 0 else {
      invalid = "CLI output permissions/ACLs are unsafe or unverifiable"
      if !reaped { interrupted = true; _ = kill(pid, SIGKILL) }
      return
    }
    if st.st_size > ProjectActivityStore.maximumOutput {
      invalid = "CLI output exceeded its size bound"
      if !reaped { interrupted = true; _ = kill(pid, SIGKILL) }
      return
    }
    var bytes = [UInt8](repeating: 0, count: 16384)
    while true {
      let count = pread(output, &bytes, bytes.count, off_t(total))
      if count < 0 && errno == EINTR { continue }
      if count < 0 { invalid = "Cannot read CLI output"; return }
      if count == 0 { return }
      total += count
      guard total <= ProjectActivityStore.maximumOutput else { invalid = "CLI output exceeded its size bound"; return }
      if invalid != nil { continue }
      pending.append(contentsOf: bytes.prefix(count))
      while let newline = pending.dropFirst(scannedBytes).firstIndex(of: 10) {
        let line = Data(pending[..<newline]); pending.removeSubrange(...newline); scannedBytes = 0
        do {
          let event = try ProjectEvent.decode(line, operation: operation)
          guard terminal == nil else { throw ProjectClientError.invalidResponse("CLI emitted data after its terminal event") }
          if let response = event.response {
            switch response {
            case .project(let project):
              guard project.name == record.projectName else { throw ProjectClientError.invalidResponse("Project result does not match request") }
            case .exports(let exports):
              guard exports.projectName == record.projectName else { throw ProjectClientError.invalidResponse("Export list does not match request") }
            case .list(let listing):
              guard listing.projects.allSatisfy({ ProjectCommand.validToken($0.name) }),
                    Set(listing.projects.map(\.name)).count == listing.projects.count else { throw ProjectClientError.invalidResponse("Invalid project discovery locators") }
            case .preview(let preview):
              guard let index = record.arguments.firstIndex(of: "--source"), index + 1 < record.arguments.count,
                    preview.source == record.arguments[index + 1] else { throw ProjectClientError.invalidResponse("Preview source does not match request") }
            case .export: break
            }
          }
          if event.type != "progress" { terminal = event }
          progress(event)
        } catch { invalid = "Malformed CLI event: " + error.localizedDescription; pending.removeAll(); scannedBytes = 0; return }
      }
      scannedBytes = pending.count
    }
  }
  private func poll() {
    guard !finished else { return }
    readAvailable()
    var status: Int32 = 0
    let observed = waitpid(pid, &status, WNOHANG)
    if observed == pid || (observed == -1 && errno != EINTR) {
      reaped = true; record.exitStatus = observed == pid ? status : nil
      readAvailable()
      let outcome: ProjectOutcome
      if let invalid { outcome = .unknown(invalid + "; refresh before retry") }
      else if interrupted { outcome = .unknown("CLI request interrupted; backend effects may have occurred. Refresh before retry") }
      else if !pending.isEmpty { outcome = .unknown("CLI ended with an incomplete JSON event; refresh before retry") }
      else if observed == pid, status == 0, let terminal, terminal.type == "result", let response = terminal.response { outcome = .success(response) }
      else if observed == pid, status != 0, let terminal, terminal.type == "error", !terminal.uncertain { outcome = .failed(terminal.message ?? "CLI request failed") }
      else { outcome = .unknown(terminal?.message ?? "CLI ended without a reliable result and exit receipt; refresh before retry") }
      finish(outcome)
    } else { queue.asyncAfter(deadline: .now() + .milliseconds(40)) { [self] in poll() } }
  }
  private func finish(_ outcome: ProjectOutcome) {
    guard !finished else { return }; finished = true
    switch outcome { case .success: record.status = .succeeded; case .failed: record.status = .failed; case .unknown: record.status = .unknown }
    record.message = outcome.message
    let final: ProjectOutcome
    do {
      var st = stat()
      guard ProjectActivityStore.privateFile(output), fstat(output, &st) == 0 else { throw ProjectClientError.activity("Cannot safely retain CLI activity") }
      if st.st_size > ProjectActivityStore.maximumOutput {
        guard ftruncate(output, off_t(ProjectActivityStore.maximumOutput)) == 0 else { throw ProjectClientError.activity("Cannot bound retained CLI output") }
      }
      guard fsync(output) == 0 else { throw ProjectClientError.activity("Cannot flush CLI activity") }
      try store.withLock { try store.save(record) }
      final = outcome
    } catch { final = .unknown("CLI exit observed but its receipt could not be retained; refresh before retry") }
    Darwin.close(output); completion(final)
  }
}
