// Original Boxwarden addition; no newer Tart source is incorporated.
import Foundation
import Combine
import Darwin

struct ClipboardTarget: Equatable {
  let cli, config, domain, session, sessionID, backendObject, generation: String

  static func validated(cli: String?, config: String?, domain: String?, session: String?,
                        sessionID: String?, backendObject: String, generation: String?,
                        noClipboard: Bool) throws -> ClipboardTarget? {
    let values = [cli, config, domain, session, sessionID, generation]
    if values.allSatisfy({ $0 == nil }) { return nil }
    guard noClipboard, values.allSatisfy({ $0 != nil }),
          absolutePath(cli!), absolutePath(config!), token(domain!), token(session!),
          token(backendObject), UUID(uuidString: sessionID!) != nil,
          UUID(uuidString: generation!) != nil else { throw ClipboardMetadataError.invalid }
    return ClipboardTarget(cli: cli!, config: config!, domain: domain!, session: session!,
                           sessionID: sessionID!, backendObject: backendObject, generation: generation!)
  }
  private static func absolutePath(_ text: String) -> Bool {
    text.hasPrefix("/") && text.utf8.count <= 4096 && !text.unicodeScalars.contains(where: { CharacterSet.controlCharacters.contains($0) }) && URL(fileURLWithPath: text).standardizedFileURL.path == text
  }
  private static func token(_ text: String) -> Bool {
    !text.isEmpty && text.utf8.count <= 255 && text.first != "-" && text.unicodeScalars.allSatisfy { CharacterSet(charactersIn: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_.-").contains($0) }
  }
  func arguments(_ direction: ClipboardDirection) -> [String] {
    ["--config", config, "--domain", domain, "clipboard", direction.rawValue, session,
     "--expected-session-id", sessionID, "--expected-backend-kind", "tart",
     "--expected-backend-object", backendObject, "--expected-generation", generation]
  }
}
enum ClipboardMetadataError: Error { case invalid }
enum ClipboardDirection: String { case push, pull }
enum ClipboardResult { case success, failed, cancelled }
@MainActor protocol ClipboardChild: AnyObject { func cancel() }
@MainActor protocol ClipboardInvoking {
  func start(target: ClipboardTarget, direction: ClipboardDirection,
             completion: @escaping @MainActor (ClipboardResult) -> Void) -> ClipboardChild
}
@MainActor final class ClipboardWindowController: ObservableObject {
  let target: ClipboardTarget?
  @Published private(set) var busy = false
  @Published private(set) var status = ""
  @Published var running = false {
    didSet { if !running { cancelOperation() } }
  }
  @Published var keyWindow = false
  private var closed = false
  private var child: ClipboardChild?
  private var operation: UUID?
  private let invoker: ClipboardInvoking
  init(target: ClipboardTarget?, invoker: ClipboardInvoking) { self.target = target; self.invoker = invoker }
  var available: Bool { target != nil && running && keyWindow && !closed && !busy }
  func perform(_ direction: ClipboardDirection) {
    guard available, let captured = target else { return }
    let id = UUID()
    operation = id; busy = true; status = "Transfer in progress."
    child = invoker.start(target: captured, direction: direction) { [weak self] result in
      guard let self = self, self.operation == id else { return }
      self.operation = nil; self.child = nil; self.busy = false
      switch result {
      case .success: self.status = "Transfer completed."
      case .failed: self.status = "Transfer failed; destination may have changed."
      case .cancelled: self.status = "Transfer cancelled; destination may have changed."
      }
    }
  }
  private func cancelOperation() {
    guard busy else { return }
    child?.cancel()
    // Retain busy until reap/completion: no queue and no second child.
  }
  func close() {
    closed = true
    cancelOperation()
    operation = nil
    status = "Window closed; transfer outcome may be unknown."
  }
}

// The viewer handles metadata only. Output goes to /dev/null (zero retained bytes).
// A dedicated queue owns spawn, cancellation, signals and waitpid, so a reaped PID
// can never be signalled after reuse. No shell, clipboard APIs, journals or retries.
@MainActor final class ClipboardProcessInvoker: ClipboardInvoking {
  func start(target: ClipboardTarget, direction: ClipboardDirection,
             completion: @escaping @MainActor (ClipboardResult) -> Void) -> ClipboardChild {
    let child = ClipboardProcessChild(completion: completion)
    child.launch(executable: target.cli, arguments: target.arguments(direction))
    return child
  }
}
@MainActor final class ClipboardProcessChild: ClipboardChild {
  private let worker: ClipboardProcessWorker
  init(completion: @escaping @MainActor (ClipboardResult) -> Void) {
    worker = ClipboardProcessWorker(completion: completion)
  }
  func launch(executable: String, arguments: [String]) { worker.launch(executable: executable, arguments: arguments) }
  func cancel() { worker.cancel() }
}
private final class ClipboardProcessWorker: @unchecked Sendable {
  private let queue = DispatchQueue(label: "boxwarden.clipboard.child")
  private let cancellationLock = NSLock()
  private var requestedCancellation = false
  private func cancellationRequested() -> Bool {
    cancellationLock.lock(); defer { cancellationLock.unlock() }; return requestedCancellation
  }
  private var pid: pid_t = 0
  private var cancelled = false
  fileprivate let id = UUID()
  private var registered = false
  private var deadline = DispatchTime.distantFuture
  private var killDeadline = DispatchTime.distantFuture
  private var sentKill = false
  private var finished = false
  private let completion: @MainActor (ClipboardResult) -> Void
  init(completion: @escaping @MainActor (ClipboardResult) -> Void) { self.completion = completion }
  func launch(executable: String, arguments: [String]) {
    registered = ClipboardProcessLifecycle.shared.register(self)
    guard registered else { queue.async { [self] in finish(.cancelled) }; return }
    queue.async { [self] in
      guard !cancellationRequested() else { finish(.cancelled); return }
      var actions: posix_spawn_file_actions_t?
      var attributes: posix_spawnattr_t?
      guard posix_spawn_file_actions_init(&actions) == 0 else { finish(.failed); return }
      defer { posix_spawn_file_actions_destroy(&actions) }
      guard posix_spawnattr_init(&attributes) == 0 else { finish(.failed); return }
      defer { posix_spawnattr_destroy(&attributes) }
      guard posix_spawnattr_setflags(&attributes, Int16(POSIX_SPAWN_CLOEXEC_DEFAULT)) == 0,
            posix_spawn_file_actions_addopen(&actions, STDIN_FILENO, "/dev/null", O_RDONLY, 0) == 0,
            posix_spawn_file_actions_addopen(&actions, STDOUT_FILENO, "/dev/null", O_WRONLY, 0) == 0,
            posix_spawn_file_actions_addopen(&actions, STDERR_FILENO, "/dev/null", O_WRONLY, 0) == 0 else { finish(.failed); return }
      let argv = ([executable] + arguments).map { strdup($0) } + [nil]
      let environmentStrings: [String] = ["PATH=/usr/bin:/bin", "LANG=en_US.UTF-8"]
      let environment = environmentStrings.map { strdup($0) } + [nil]
      defer { argv.forEach { free($0) }; environment.forEach { free($0) } }
      guard !cancellationRequested() else { finish(.cancelled); return }
      let result = argv.withUnsafeBufferPointer { av in
        environment.withUnsafeBufferPointer { ev in
          posix_spawn(&pid, executable, &actions, &attributes,
                      UnsafeMutablePointer(mutating: av.baseAddress!), UnsafeMutablePointer(mutating: ev.baseAddress!))
        }
      }
      guard result == 0 else { pid = 0; finish(.failed); return }
      deadline = .now() + .seconds(30)
      poll()
    }
  }
  func cancel() {
    cancellationLock.lock(); requestedCancellation = true; cancellationLock.unlock()
    queue.async { [self] in requestCancel() }
  }
  private func requestCancel() {
    guard !finished, !cancelled else { return }
    cancelled = true
    if pid > 0 { kill(pid, SIGTERM) }
    killDeadline = .now() + .seconds(1)
  }
  private func poll() {
    guard !finished, pid > 0 else { return }
    var status: Int32 = 0
    let observed = waitpid(pid, &status, WNOHANG)
    if observed == pid {
      pid = 0
      finish(cancelled ? .cancelled : (status == 0 ? .success : .failed)); return
    }
    if observed == -1 && errno != EINTR { pid = 0; finish(.failed); return }
    if DispatchTime.now() >= deadline { requestCancel() }
    if cancelled && DispatchTime.now() >= killDeadline && !sentKill { kill(pid, SIGKILL); sentKill = true }
    queue.asyncAfter(deadline: .now() + .milliseconds(25)) { [self] in poll() }
  }
  private func finish(_ result: ClipboardResult) {
    guard !finished else { return }
    finished = true
    if registered { ClipboardProcessLifecycle.shared.finished(id) }
    Task { @MainActor [completion] in completion(result) }
  }
}
// Process-wide lifetime only: tracks owned children for normal exit. No payload,
// target lookup, clipboard authority, transport or persistent state lives here.
final class ClipboardProcessLifecycle: @unchecked Sendable {
  static let shared = ClipboardProcessLifecycle()
  private let lock = NSLock()
  private let drained = DispatchGroup()
  private var closing = false
  private var workers: [UUID: ClipboardProcessWorker] = [:]
  fileprivate func register(_ worker: ClipboardProcessWorker) -> Bool {
    lock.lock(); defer { lock.unlock() }
    guard !closing else { return false }
    drained.enter(); workers[worker.id] = worker; return true
  }
  fileprivate func finished(_ id: UUID) {
    lock.lock()
    let removed = workers.removeValue(forKey: id) != nil
    lock.unlock()
    if removed { drained.leave() }
  }
  private func beginShutdown() {
    lock.lock(); closing = true; let owned = Array(workers.values); lock.unlock()
    owned.forEach { $0.cancel() }
  }
  func shutdown() async {
    beginShutdown()
    await withCheckedContinuation { continuation in
      drained.notify(queue: .global()) { continuation.resume() }
    }
  }
}

extension ClipboardProcessLifecycle {
  func installStopSignals(onStop: @escaping @Sendable () -> Void) -> [DispatchSourceSignal] {
    // All normal catchable process termination requests use one drain path.
    [SIGINT, SIGTERM, SIGHUP].map { number in
      signal(number, SIG_IGN)
      let source = DispatchSource.makeSignalSource(signal: number, queue: .global())
      source.setEventHandler {
        Task { await self.shutdown(); onStop() }
      }
      source.activate()
      return source
    }
  }
}
