import Foundation
import Darwin

@main struct QueryLifecycleTests {
  static func check(_ value: @autoclosure () throws -> Bool, _ message: String) {
      guard (try! value()) else { fputs("FAIL: \(message)\n", stderr); exit(1) }
    }
  static func main() throws {
    let resolved = realpath(FileManager.default.temporaryDirectory.path, nil)!
    defer { free(resolved) }
    let root = URL(fileURLWithPath: String(cString: resolved)).appendingPathComponent("bw-query-lifecycle-" + UUID().uuidString)
    try FileManager.default.createDirectory(at: root, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
    defer { try? FileManager.default.removeItem(at: root) }
    let cli = root.appendingPathComponent("cli"), mutationStarted = root.appendingPathComponent("mutation-started"), queryStarted = root.appendingPathComponent("query-started")
    let body = """
    #!/bin/sh
    if [ "$6" = open ] || [ "$6" = push ]; then
      /usr/bin/touch '\(mutationStarted.path)'
      /bin/sleep 3
      if [ "$6" = open ]; then
        printf '%s\\n' '{"version":1,"type":"error","operation":"project.open","message":"synthetic admission failure","data":{"uncertain":false}}'
        exit 3
      fi
      exit 0
    fi
    /usr/bin/touch '\(queryStarted.path)'
    trap '' TERM
    exec /bin/sleep 60
    """
    try Data(body.utf8).write(to: cli)
    try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: cli.path)
    func waitFor(_ file: URL) {
      let end = Date().addingTimeInterval(4)
      while !FileManager.default.fileExists(atPath: file.path), Date() < end { usleep(5_000) }
      check(FileManager.default.fileExists(atPath: file.path), "exact synthetic child reached launch checkpoint: \(file.lastPathComponent)")
    }
    let client = try ProjectClient(executable: cli.path, config: "/synthetic/config", activityDirectory: root.appendingPathComponent("activity"))
    let mutationDone = DispatchSemaphore(value: 0)
    let mutation = try client.start(.open(name: "one")) { outcome in
      if case .failed = outcome {} else { check(false, "query cleanup must not change mutation outcome") }
      mutationDone.signal()
    }
    waitFor(mutationStarted)
    let requests = DispatchGroup()
    for _ in 0..<32 {
      requests.enter()
      DispatchQueue.global().async {
        defer { requests.leave() }
        do { _ = try client.query(.list); check(false, "superseded query cannot succeed") } catch {}
      }
    }
    waitFor(queryStarted)
    let drained = DispatchSemaphore(value: 0), beforeDrain = Date()
    client.disposeQueries { drained.signal() }
    check(drained.wait(timeout: .now() + .seconds(4)) == .success, "query disposal escalates TERM-ignoring child and observes reap")
    check(Date().timeIntervalSince(beforeDrain) < 2, "query drain does not wait for durable mutation lifetime")
    check(requests.wait(timeout: .now() + .seconds(4)) == .success, "queued obsolete queries fail without launching")
    check(try client.recoverActivities().filter { $0.operation == "project.list" }.count == 1, "serialized pending requests do not create more query children during disposal")
    check(try client.recoverActivities().first(where: { $0.id == mutation.id })?.status == .running, "mutation remains running after query-only cleanup")
    let reopened = try ProjectClient(executable: cli.path, config: "/synthetic/config", activityDirectory: root.appendingPathComponent("activity"))
    do { try reopened.start(.open(name: "one")) { _ in }; check(false, "reopen cannot duplicate live mutation after cleanup") } catch {}
    do { _ = try client.query(.list); check(false, "disposed client admits future queries") } catch {}
    do { try client.start(.list) { _ in }; check(false, "asynchronous query start bypasses disposal admission") } catch {}
    check(mutationDone.wait(timeout: .now() + .seconds(4)) == .success, "mutation retains normal independent completion")
    check(try reopened.recoverActivities().first(where: { $0.id == mutation.id })?.status == .failed, "mutation terminal plus exit receipt remains reliable after query cleanup")

    let deferred = try ProjectClient(executable: cli.path, config: "/synthetic/deferred", activityDirectory: root.appendingPathComponent("deferred"))
    let stale = deferred.queryToken()
    deferred.cancelQueries()
    do { _ = try deferred.query(.list, queryToken: stale); check(false, "deferred old query can enter after an effect supersedes it") } catch {}
    check(try deferred.recoverActivities().isEmpty, "obsolete generation does not spawn or create a query receipt")

    // Repeated spawn/disposal races exercise atomic query owner registration.
    for index in 0..<24 {
      let racing = try ProjectClient(executable: cli.path, config: "/synthetic/race", activityDirectory: root.appendingPathComponent("race-\(index)"))
      let returned = DispatchSemaphore(value: 0), stopped = DispatchSemaphore(value: 0)
      DispatchQueue.global().async { defer { returned.signal() }; _ = try? racing.query(.list) }
      if index % 2 == 0 { usleep(1_000) }
      racing.disposeQueries { stopped.signal() }
      check(stopped.wait(timeout: .now() + .seconds(4)) == .success && returned.wait(timeout: .now() + .seconds(4)) == .success, "dispose during spawn cannot strand an owned query")
      check(try racing.recoverActivities().allSatisfy { $0.status != .running }, "drain observes every admitted query reaped")
    }

    try FileManager.default.removeItem(at: queryStarted)
    let clipboard = CLIClient(executable: cli.standardizedFileURL.path, config: "/synthetic/config")
    let target = MenuTarget(domain: "alpha", session: "one", sessionID: "11111111-1111-4111-8111-111111111111", backendKind: "tart", backendObject: "one", generation: "22222222-2222-4222-8222-222222222222", available: true)
    let transferDone = DispatchSemaphore(value: 0)
    _ = clipboard.transfer(TransferRequest(id: UUID(), target: target, direction: .push)) { outcome in
      check(outcome == .success, "discovery-only disposal preserves explicit transfer")
      transferDone.signal()
    }
    let obsoleteClipboard = clipboard.queryToken()
    clipboard.cancelQueries()
    do { _ = try clipboard.discover(domain: "alpha", queryToken: obsoleteClipboard); check(false, "old second-stage discovery enters a new cancellation generation") } catch {}
    let discovered = DispatchSemaphore(value: 0), clipboardDrained = DispatchSemaphore(value: 0)
    DispatchQueue.global().async { do { _ = try clipboard.discover(domain: "alpha") } catch { fputs("Discovery fixture: \(error)\n", stderr) }; discovered.signal() }
    waitFor(queryStarted)
    clipboard.disposeQueries { clipboardDrained.signal() }
    check(clipboardDrained.wait(timeout: .now() + .seconds(4)) == .success && discovered.wait(timeout: .now() + .seconds(4)) == .success, "clipboard discovery drains exact owned process")
    do { _ = try clipboard.discover(domain: "alpha"); check(false, "disposed clipboard client admits discovery") } catch {}
    check(transferDone.wait(timeout: .now() + .seconds(4)) == .success, "explicit transfer completes independently after query drain")
    print("PASS: query drain, queued invalidation, spawn/dispose races, durable mutation and explicit clipboard transfer preservation")
  }
}
