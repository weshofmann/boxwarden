import Foundation
import Darwin

@main struct ActivityLockTests {
  static func check(_ value: Bool, _ message: String) {
    guard value else { fputs("FAIL: \(message)\n", stderr); exit(1) }
  }
  static func main() throws {
    let resolved = realpath(FileManager.default.temporaryDirectory.path, nil)!
    defer { free(resolved) }
    let root = URL(fileURLWithPath: String(cString: resolved)).appendingPathComponent("bw-activity-lock-" + UUID().uuidString)
    try FileManager.default.createDirectory(at: root, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
    defer { try? FileManager.default.removeItem(at: root) }
    let cli = root.appendingPathComponent("cli"), activityRoot = root.appendingPathComponent("activity")
    try Data("#!/bin/sh\nexec /bin/sleep 60\n".utf8).write(to: cli)
    try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: cli.path)
    let client = try ProjectClient(executable: cli.path, config: "/synthetic/config", activityDirectory: activityRoot)
    let held = open(activityRoot.appendingPathComponent(".lock").path, O_RDWR | O_CLOEXEC)
    check(held >= 0 && flock(held, LOCK_EX | LOCK_NB) == 0, "synthetic owner holds exact task-private activity lock")
    defer { close(held) }
    let queryDone = DispatchSemaphore(value: 0), disposeReturned = DispatchSemaphore(value: 0), drained = DispatchSemaphore(value: 0)
    DispatchQueue.global().async { _ = try? client.query(.list); queryDone.signal() }
    usleep(100_000)
    DispatchQueue.global().async { client.disposeQueries { drained.signal() }; disposeReturned.signal() }
    let responsive = disposeReturned.wait(timeout: .now() + .milliseconds(200)) == .success
    _ = flock(held, LOCK_UN)
    check(queryDone.wait(timeout: .now() + .seconds(4)) == .success && drained.wait(timeout: .now() + .seconds(4)) == .success, "held-lock fixture released and query cleanup completed")
    check(responsive, "query disposal must not wait for another activity-lock owner")

    check(flock(held, LOCK_EX | LOCK_NB) == 0, "reacquire synthetic private lock")
    let recoveryDone = DispatchSemaphore(value: 0)
    DispatchQueue.global().async { _ = try? client.recoverActivities(); recoveryDone.signal() }
    let recoveryResponsive = recoveryDone.wait(timeout: .now() + .milliseconds(200)) == .success
    _ = flock(held, LOCK_UN)
    check(recoveryDone.wait(timeout: .now() + .seconds(2)) == .success || recoveryResponsive, "recovery fixture released")
    check(recoveryResponsive, "activity recovery must fail busy without waiting on another frontend")
    print("PASS: external command activity lock cannot block query disposal or main-thread recovery")
  }
}
