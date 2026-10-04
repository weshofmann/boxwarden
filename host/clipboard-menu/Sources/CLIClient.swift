import Foundation
import Darwin

enum CLIError: Error { case invalidLocator, invalidResponse, processFailed }

struct DiscoveryResult {
  let targets: [MenuTarget]
  let failedDomains: Int
}

final class CLIClient {
  let executable: String
  let config: String
  let deadlineSeconds: Double
  let privateHostPasteboard: String?
  private let lock = NSLock()
  private let drained = DispatchGroup()
  private var closing = false
  private var children: [UUID: RunningProcess] = [:]
  init(executable: String, config: String, deadlineSeconds: Double = 30, privateHostPasteboard: String? = nil) {
    self.executable = executable; self.config = config; self.deadlineSeconds = deadlineSeconds
    self.privateHostPasteboard = privateHostPasteboard
  }

  static func validPath(_ path: String) -> Bool {
    path.hasPrefix("/") && path.utf8.count <= 4096 &&
    !path.unicodeScalars.contains(where: { CharacterSet.controlCharacters.contains($0) }) &&
    URL(fileURLWithPath: path).standardizedFileURL.path == path
  }

  static func validPrivatePasteboard(_ name: String) -> Bool {
    name.hasPrefix("org.boxwarden.test.") && name.utf8.count > "org.boxwarden.test.".utf8.count && name.utf8.count <= 255 &&
      name.utf8.allSatisfy { (48...57).contains($0) || (65...90).contains($0) || (97...122).contains($0) || $0 == 46 || $0 == 95 || $0 == 45 }
  }

  private func register(_ child: RunningProcess) -> Bool {
    lock.lock(); defer { lock.unlock() }
    guard !closing else { return false }
    children[child.id] = child
    drained.enter()
    return true
  }
  private func finished(_ id: UUID) {
    lock.lock()
    let removed = children.removeValue(forKey: id) != nil
    lock.unlock()
    if removed { drained.leave() }
  }
  func shutdown(_ completion: @escaping () -> Void) {
    lock.lock(); closing = true; let owned = Array(children.values); lock.unlock()
    owned.forEach { $0.cancel() }
    drained.notify(queue: .global(), execute: completion)
  }

  func discover(domain: String) throws -> [MenuTarget] {
    guard Self.validPath(executable), Self.validPath(config), MenuTarget.validToken(domain) else { throw CLIError.invalidLocator }
    let done = DispatchSemaphore(value: 0)
    var response = Data()
    var result: TransferResult = .failed
    let id = UUID()
    let child = RunningProcess(id: id, readOutput: true, deadlineSeconds: deadlineSeconds) { [self] data, outcome in
      response = data; result = outcome; finished(id); done.signal()
    }
    guard register(child) else { throw CLIError.processFailed }
    child.start(executable: executable, arguments: ["--config", config, "--domain", domain, "clipboard", "targets"])
    done.wait()
    guard result == .success else { throw CLIError.processFailed }
    let decoded = try JSONDecoder().decode(TargetResponse.self, from: response)
    guard decoded.targets.allSatisfy({ $0.domain == domain && $0.isValid }) else { throw CLIError.invalidResponse }
    return decoded.targets
  }

  func discoverConfiguredDomains(from configData: Data) throws -> DiscoveryResult {
    let domains = try ConfigDomains.names(from: configData)
    var targets: [MenuTarget] = []
    var failures = 0
    for domain in domains {
      do { targets.append(contentsOf: try discover(domain: domain)) }
      catch { failures += 1 }
    }
    return DiscoveryResult(targets: targets, failedDomains: failures)
  }

  func transfer(_ request: TransferRequest, completion: @escaping (TransferResult) -> Void) -> RunningProcess? {
    guard Self.validPath(executable), Self.validPath(config), request.target.isValid, request.target.available else { return nil }
    if let board = privateHostPasteboard, !Self.validPrivatePasteboard(board) { return nil }
    var arguments = request.arguments(config: config)
    if let board = privateHostPasteboard { arguments += ["--private-host-pasteboard", board] }
    let id = UUID()
    let child = RunningProcess(id: id, readOutput: false, deadlineSeconds: deadlineSeconds) { [self] _, result in
      completion(result); finished(id)
    }
    guard register(child) else { return nil }
    child.start(executable: executable, arguments: arguments)
    return child
  }
}

// One serial queue owns spawn, signalling, waitpid and reaping. A PID is never
// signalled after its owner has been reaped, so reuse cannot retarget a kill.
final class RunningProcess: @unchecked Sendable {
  let id: UUID
  private let queue = DispatchQueue(label: "boxwarden.clipboard-menu.child")
  private let cancellationLock = NSLock()
  private var cancellationRequested = false
  private var pid: pid_t = 0
  private var outputFD: Int32 = -1
  private var output = Data()
  private let readOutput: Bool
  private var sawEOF: Bool
  private var reaped = false
  private var exitStatus: Int32 = -1
  private var stopped = false
  private var userCancelled = false
  private var sentKill = false
  private var finished = false
  private var deadline = DispatchTime.distantFuture
  private var killDeadline = DispatchTime.distantFuture
  private let deadlineSeconds: Double
  private let completion: (Data, TransferResult) -> Void

  init(id: UUID, readOutput: Bool, deadlineSeconds: Double,
       completion: @escaping (Data, TransferResult) -> Void) {
    self.id = id
    self.readOutput = readOutput
    self.sawEOF = !readOutput
    self.deadlineSeconds = deadlineSeconds
    self.completion = completion
  }

  func start(executable: String, arguments: [String]) {
    queue.async { [self] in launch(executable: executable, arguments: arguments) }
  }

  func cancel() {
    cancellationLock.lock(); cancellationRequested = true; cancellationLock.unlock()
    queue.async { [self] in requestStop(user: true) }
  }

  private func wasCancelled() -> Bool {
    cancellationLock.lock(); defer { cancellationLock.unlock() }; return cancellationRequested
  }

  private func launch(executable: String, arguments: [String]) {
    guard !wasCancelled() else { finish(.cancelled); return }
    var pipeFDs: [Int32] = [-1, -1]
    if readOutput && pipe(&pipeFDs) != 0 { finish(.failed); return }
    var actions: posix_spawn_file_actions_t?
    var attributes: posix_spawnattr_t?
    guard posix_spawn_file_actions_init(&actions) == 0 else { closePipe(pipeFDs); finish(.failed); return }
    defer { posix_spawn_file_actions_destroy(&actions) }
    guard posix_spawnattr_init(&attributes) == 0 else { closePipe(pipeFDs); finish(.failed); return }
    defer { posix_spawnattr_destroy(&attributes) }
    let stdoutReady: Bool
    if readOutput {
      stdoutReady = posix_spawn_file_actions_addclose(&actions, pipeFDs[0]) == 0 &&
        posix_spawn_file_actions_adddup2(&actions, pipeFDs[1], STDOUT_FILENO) == 0 &&
        posix_spawn_file_actions_addclose(&actions, pipeFDs[1]) == 0
    } else {
      stdoutReady = posix_spawn_file_actions_addopen(&actions, STDOUT_FILENO, "/dev/null", O_WRONLY, 0) == 0
    }
    guard posix_spawnattr_setflags(&attributes, Int16(POSIX_SPAWN_CLOEXEC_DEFAULT)) == 0,
          posix_spawn_file_actions_addopen(&actions, STDIN_FILENO, "/dev/null", O_RDONLY, 0) == 0,
          stdoutReady,
          posix_spawn_file_actions_addopen(&actions, STDERR_FILENO, "/dev/null", O_WRONLY, 0) == 0 else {
      closePipe(pipeFDs); finish(.failed); return
    }
    let argv = ([executable] + arguments).map { strdup($0) } + [nil]
    let environmentStrings: [String] = ["PATH=/usr/bin:/bin", "LANG=en_US.UTF-8"]
    let environment = environmentStrings.map { strdup($0) } + [nil]
    defer { argv.forEach { free($0) }; environment.forEach { free($0) } }
    guard !wasCancelled() else { closePipe(pipeFDs); finish(.cancelled); return }
    let result = argv.withUnsafeBufferPointer { av in
      environment.withUnsafeBufferPointer { ev in
        posix_spawn(&pid, executable, &actions, &attributes,
                    UnsafeMutablePointer(mutating: av.baseAddress!), UnsafeMutablePointer(mutating: ev.baseAddress!))
      }
    }
    if readOutput {
      Darwin.close(pipeFDs[1])
      if result == 0 {
        outputFD = pipeFDs[0]
        _ = fcntl(outputFD, F_SETFL, O_NONBLOCK)
      } else { Darwin.close(pipeFDs[0]) }
    }
    guard result == 0 else { pid = 0; finish(.failed); return }
    deadline = .now() + .milliseconds(max(1, Int(deadlineSeconds * 1000)))
    poll()
  }

  private func closePipe(_ fds: [Int32]) { for fd in fds where fd >= 0 { Darwin.close(fd) } }

  private func requestStop(user: Bool) {
    guard !finished else { return }
    if user { userCancelled = true }
    guard !stopped else { return }
    stopped = true
    if pid > 0 { kill(pid, SIGTERM) }
    killDeadline = .now() + .seconds(1)
  }

  private func readAvailable() {
    guard outputFD >= 0 else { return }
    var bytes = [UInt8](repeating: 0, count: 8192)
    while true {
      let count = Darwin.read(outputFD, &bytes, bytes.count)
      if count > 0 {
        output.append(contentsOf: bytes.prefix(count))
        if output.count > 1 << 20 {
          requestStop(user: false)
          Darwin.close(outputFD); outputFD = -1; sawEOF = true
          return
        }
      } else if count == 0 {
        Darwin.close(outputFD); outputFD = -1; sawEOF = true; return
      } else if errno == EINTR { continue }
      else if errno == EAGAIN { return }
      else { requestStop(user: false); Darwin.close(outputFD); outputFD = -1; sawEOF = true; return }
    }
  }

  private func poll() {
    guard !finished else { return }
    readAvailable()
    if pid > 0 {
      var status: Int32 = 0
      let observed = waitpid(pid, &status, WNOHANG)
      if observed == pid { pid = 0; reaped = true; exitStatus = status }
      else if observed == -1 && errno != EINTR { pid = 0; reaped = true; exitStatus = -1 }
    }
    if !stopped && DispatchTime.now() >= deadline { requestStop(user: false) }
    if stopped && pid > 0 && DispatchTime.now() >= killDeadline && !sentKill { kill(pid, SIGKILL); sentKill = true }
    if reaped {
      readAvailable()
      if !sawEOF { Darwin.close(outputFD); outputFD = -1; sawEOF = true; exitStatus = -1 }
      finish(userCancelled ? .cancelled : (!stopped && exitStatus == 0 ? .success : .failed))
      return
    }
    queue.asyncAfter(deadline: .now() + .milliseconds(25)) { [self] in poll() }
  }

  private func finish(_ result: TransferResult) {
    guard !finished else { return }
    finished = true
    if outputFD >= 0 { Darwin.close(outputFD); outputFD = -1 }
    completion(output, result)
  }
}
