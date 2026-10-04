import Foundation
import Darwin

@main struct ProjectClientTests {
  static func check(_ value: @autoclosure () throws -> Bool, _ message: String) {
    if !(try! value()) { fputs("FAIL: \(message)\n", stderr); exit(1) }
  }
  static func main() throws {
    if CommandLine.arguments.count == 4 && CommandLine.arguments[1] == "--detach" {
      let detached = try ProjectClient(executable: CommandLine.arguments[2], config: "/opt/config with spaces", activityDirectory: URL(fileURLWithPath: CommandLine.arguments[3]))
      try detached.start(.open(name: "detached")) { _ in }
      exit(0)
    }
    let actualTemporary = realpath(FileManager.default.temporaryDirectory.path, nil)!
    defer { free(actualTemporary) }
    let root = URL(fileURLWithPath: String(cString: actualTemporary)).appendingPathComponent("bw-project-client-" + UUID().uuidString)
    try FileManager.default.createDirectory(at: root, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
    defer { try? FileManager.default.removeItem(at: root) }
    let cli = root.appendingPathComponent("cli")
    func script(_ body: String) throws {
      try Data(("#!/bin/sh\n" + body).utf8).write(to: cli)
      try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: cli.path)
    }
    let listing = #"{"version":1,"type":"result","operation":"project.list","data":{"setup":{"status":"missing","guidance":"Prepare host explicitly"},"projects":[]}}"#
    try script("[ -z \"$BOXWARDEN_TEST_SECRET\" ] || exit 31\n[ \"$1\" = --config ] && [ \"$2\" = '/opt/config with spaces' ] && [ \"$3\" = --domain ] && [ \"$4\" = alpha ] && [ \"$5\" = project ] && [ \"$6\" = list ] && [ \"$7\" = --json ] || exit 32\nprintf '%s\\n' '\(listing)'\n")
    setenv("BOXWARDEN_TEST_SECRET", "must not inherit", 1)
    defer { unsetenv("BOXWARDEN_TEST_SECRET") }
    let activity = root.appendingPathComponent("activity")
    let client = try ProjectClient(executable: cli.path, config: "/opt/config with spaces", activityDirectory: activity)
    if case .list(let value) = try client.query(.list) {
      check(value.projects.isEmpty && value.setup.status == "missing", "typed setup state and fixed argv with closed environment")
    } else { check(false, "wrong response type") }
    let project = #"{"name":"demo","base":"base","session_id":"session","backend_object":"object","state":"ready","management_ready":true,"diagnostic":"","observed_state":"running","backend_running":true,"workspace":{"id":"workspace","filesystem_uuid":"fs","size_bytes":536870912,"mount_path":"/workspace","initialized":true},"software":{"intent_digest":"digest","status":"complete","actions":[{"action_id":"a","phase":"install","state":"complete","generation":"g"}]},"import":{"status":"not_imported","id":"","guest_path":""},"replacement_pending":false,"available_actions":["open","status","stop","import","export"]}"#
    let openResult = "{\"version\":1,\"type\":\"result\",\"operation\":\"project.open\",\"data\":{\"project\":\(project)}}"
    func run(_ command: ProjectCommand) throws -> ProjectOutcome {
      let done = DispatchSemaphore(value: 0); var outcome: ProjectOutcome?
      try client.start(command) { result in outcome = result; done.signal() }
      check(done.wait(timeout: .now() + .seconds(5)) == .success, "child completion bounded in synthetic test")
      return outcome!
    }
    try script("printf '%s\\n' '\(openResult)'\n")
    if case .success(.project(let value)) = try run(.open(name: "demo")) {
      check(value.name == "demo" && value.workspace.sizeBytes == 536870912 && value.importState.status == "not_imported" && value.software.actions.first?.actionId == "a", "typed nested project fields")
    } else { check(false, "successful typed project request") }
    try script("printf '%s\\n' '\(openResult)'\n")
    if case .unknown = try run(.open(name: "wrong-name")) {} else { check(false, "mismatched project result cannot be accepted") }
    let commandCases: [(ProjectCommand, String, [String])] = [
      (.create(name: "demo", recipe: "desktop", sizeMiB: 512), "project.create", ["--config", "/opt/config with spaces", "--domain", "alpha", "project", "create", "--json", "--recipe", "desktop", "--size-mib", "512", "demo"]),
      (.rebuild(name: "demo", recipe: "actions"), "project.rebuild", ["--config", "/opt/config with spaces", "--domain", "alpha", "project", "rebuild", "--json", "--recipe", "actions", "demo"]),
      (.importProject(name: "demo", source: "/opt/source with spaces", exclusions: ["build cache"], previewDigest: String(repeating: "d", count: 64)), "project.import", ["--config", "/opt/config with spaces", "--domain", "alpha", "project", "import", "--json", "--source", "/opt/source with spaces", "--exclude", "build cache", "--expected-digest", String(repeating: "d", count: 64), "demo"])
    ]
    for (command, operation, expected) in commandCases {
      let argumentChecks = expected.enumerated().map { "[ \"${\($0.offset + 1)}\" = '\($0.element)' ] || exit 23" }.joined(separator: "\n")
      let terminal = "{\"version\":1,\"type\":\"result\",\"operation\":\"\(operation)\",\"data\":{\"project\":\(project)}}"
      try script("[ \"$#\" -eq \(expected.count) ] || exit 22\n" + argumentChecks + "\nprintf '%s\\n' '\(terminal)'\n")
      if case .success = try run(command) {} else { check(false, "recipe, size and preview-digest argv preserved for \(operation)") }
    }
    for body in [
      "printf '%s\\n' 'garbage'\n",
      "printf '%s\\n' '{\"version\":2,\"type\":\"result\",\"operation\":\"project.list\",\"data\":{}}'\n",
      "printf '%s\\n' '{\"version\":1,\"type\":\"result\",\"operation\":\"project.status\",\"data\":{}}'\n",
      "printf '%s' '\(listing)'\n",
      "printf '%s\\n' '\(listing)'\nexit 4\n",
      "printf '%s\\n' '\(listing)' '\(listing)'\n"
    ] {
      try script(body)
      do { _ = try client.query(.list); check(false, "malformed, partial, wrong-version, wrong-operation, duplicate-terminal or failed-exit result accepted") } catch {}
    }
    try script("printf '%s\\n' '{\"version\":1,\"type\":\"error\",\"operation\":\"project.open\",\"message\":\"admission rejected\",\"data\":{\"uncertain\":false}}'\nexit 5\n")
    if case .failed(let message) = try run(.open(name: "rejected")) { check(message == "admission rejected", "useful structured errors") }
    else { check(false, "certain CLI error classified") }
    try script("printf '%s\\n' '{\"version\":1,\"type\":\"error\",\"operation\":\"project.open\",\"message\":\"effect uncertain\",\"data\":{\"uncertain\":true}}'\nexit 5\n")
    if case .unknown = try run(.open(name: "uncertain")) {} else { check(false, "uncertain CLI error classified") }
    do { try client.start(.open(name: "uncertain")) { _ in }; check(false, "unknown activity automatically replayed") } catch {}
    let retried = DispatchSemaphore(value: 0)
    let retriedResult = openResult.replacingOccurrences(of: "\"name\":\"demo\"", with: "\"name\":\"uncertain\"")
    try script("printf '%s\\n' '\(retriedResult)'\n")
    try client.start(.open(name: "uncertain"), allowRetryAfterUnknown: true) { _ in retried.signal() }
    check(retried.wait(timeout: .now() + .seconds(5)) == .success, "explicit retry passes backend admission")
    let afterAcknowledgement = DispatchSemaphore(value: 0)
    do { try ProjectClient(executable: cli.path, config: "/opt/config with spaces", activityDirectory: activity).start(.open(name: "uncertain")) { _ in afterAcknowledgement.signal() } }
    catch { check(false, "acknowledged historical unknown must not block later explicit effects") }
    check(afterAcknowledgement.wait(timeout: .now() + .seconds(5)) == .success, "acknowledgement persists across client instances")
    check(try client.recoverActivities().contains(where: { $0.projectName == "uncertain" && $0.status == .unknown }), "acknowledgement preserves original unknown outcome")

    for (index, dataField) in ["", ",\"data\":{}", ",\"data\":{\"uncertain\":null}", ",\"data\":{\"uncertain\":\"false\"}", ",\"data\":[]"].enumerated() {
      try script("printf '%s\\n' '{\"version\":1,\"type\":\"error\",\"operation\":\"project.open\",\"message\":\"missing reliable uncertainty\"\(dataField)}'\nexit 5\n")
      let name = "omitted-uncertainty-" + String(index)
      if case .unknown = try run(.open(name: name)) {} else { check(false, "error without explicit Boolean false cannot classify known failure") }
      do { try client.start(.open(name: name)) { _ in }; check(false, "omitted uncertainty permits mutation replay") } catch {}
    }
    let progressEvent = #"{"version":1,"type":"progress","operation":"project.open","message":"Preparing synthetic project"}"#
    let receivedProgress = DispatchSemaphore(value: 0), completed = DispatchSemaphore(value: 0)
    try script("printf '%s\\n' '\(progressEvent)'\n/bin/sleep 0.5\nprintf '%s\\n' '\(openResult)'\n")
    let running = try client.start(.open(name: "demo"), progress: { event in if event.type == "progress" { receivedProgress.signal() } }) { _ in completed.signal() }
    check(!client.cancelActiveActivity(running.id), "non-export mutation cannot be interrupted through export control")
    check(receivedProgress.wait(timeout: .now() + .seconds(3)) == .success, "progress delivered before completion")
    let secondClient = try ProjectClient(executable: cli.path, config: "/opt/config with spaces", activityDirectory: activity)
    check(try secondClient.recoverActivities().contains(where: { $0.id == running.id && $0.status == .running }), "reopen observes exact live process identity")
    do { try secondClient.start(.open(name: "demo")) { _ in }; check(false, "second client duplicates active effects") } catch {}
    check(completed.wait(timeout: .now() + .seconds(4)) == .success, "owned process completes")
    check(try secondClient.recoverActivities().contains(where: { $0.id == running.id && $0.status == .succeeded }), "terminal event plus exit persisted")
    check(try secondClient.activityEvents(running.id).count == 2, "retained JSONL progress readable after reopen")

    let detachedResult = openResult.replacingOccurrences(of: "\"name\":\"demo\"", with: "\"name\":\"detached\"")
    let marker = root.appendingPathComponent("detached-completed")
    try script("printf '%s\\n' '\(progressEvent)'\n/bin/sleep 0.6\nprintf '%s\\n' '\(detachedResult)'\n/usr/bin/touch '\(marker.path)'\n")
    let frontend = Process(); frontend.executableURL = URL(fileURLWithPath: CommandLine.arguments[0])
    frontend.arguments = ["--detach", cli.path, activity.path]
    try frontend.run(); frontend.waitUntilExit()
    check(frontend.terminationStatus == 0, "synthetic frontend exited")
    let detachedActivity = try secondClient.recoverActivities().first(where: { $0.projectName == "detached" })!
    check(detachedActivity.status == .running, "quitting frontend does not cancel owned CLI")
    do { try secondClient.start(.open(name: "detached")) { _ in }; check(false, "relaunch duplicated detached mutation") } catch {}
    let deadline = Date().addingTimeInterval(4)
    while !FileManager.default.fileExists(atPath: marker.path) && Date() < deadline { usleep(20_000) }
    check(FileManager.default.fileExists(atPath: marker.path), "detached CLI finished observable effects after frontend exit")
    check(try secondClient.recoverActivities().contains(where: { $0.id == detachedActivity.id && $0.status == .unknown }), "terminal event without retained exit cannot claim success")
    check(try secondClient.activityEvents(detachedActivity.id).last?.type == "result", "detached output survives app exit")

    try script("exec /usr/bin/head -c 9000000 /dev/zero\n")
    do { _ = try client.query(.list); check(false, "oversized output accepted") } catch {}
    let oversizedActivity = try secondClient.recoverActivities().first(where: { $0.operation == "project.list" })!
    let oversizedSize = try FileManager.default.attributesOfItem(atPath: oversizedActivity.directory.appendingPathComponent("events.jsonl").path)[.size] as! NSNumber
    check(oversizedSize.intValue <= 8 * 1024 * 1024, "oversized retained output stays bounded")
    try script("printf '%s\\n' '\(listing)'\n")
    for _ in 0..<24 { do { _ = try client.query(.list) } catch {} }
    let queryReceipts = try secondClient.recoverActivities().filter { $0.operation == "project.list" }
    check(queryReceipts.count <= 17, "refresh queries retain a bounded number of completed activities")
    let otherConfig = try ProjectClient(executable: cli.path, config: "/opt/other config", activityDirectory: activity)
    check(try otherConfig.recoverActivities().isEmpty, "activity rediscovery is scoped to exact config and domain")
    let previewPath = root.appendingPathComponent("preview.jsonl")
    let previewEntries: [[String: Any]] = (0..<8192).map { ["path": String(repeating: "a", count: 500) + String($0), "kind": "file", "size": 1, "sha256": String(repeating: "b", count: 64)] }
    let previewObject: [String: Any] = ["version": 1, "type": "result", "operation": "project.import.preview", "data": ["source": "/opt/project with spaces", "entries": previewEntries, "file_count": 8192, "directory_count": 0, "total_bytes": 8192, "digest": String(repeating: "c", count: 64), "exclusions": ["build cache"]]]
    var previewData = try JSONSerialization.data(withJSONObject: previewObject); previewData.append(10)
    check(previewData.count > 4 * 1024 * 1024 && previewData.count < 8 * 1024 * 1024, "large preview fixture exercises supported output size")
    try previewData.write(to: previewPath)
    try script("[ \"$6\" = import ] && [ \"$7\" = preview ] && [ \"$8\" = --json ] && [ \"$9\" = --source ] && [ \"${10}\" = '/opt/project with spaces' ] && [ \"${11}\" = --exclude ] && [ \"${12}\" = 'build cache' ] || exit 21\nexec /bin/cat '\(previewPath.path)'\n")
    if case .preview(let preview) = try client.query(.importPreview(source: "/opt/project with spaces", exclusions: ["build cache"])) {
      check(preview.entries.count == 8192 && preview.totalBytes == 8192 && preview.exclusions == ["build cache"], "large preview survives streaming and retains exact typed counts")
    } else { check(false, "typed large preview") }
    try script("exec /bin/sleep 60\n")
    let cancelDone = DispatchSemaphore(value: 0); var cancelOutcome: ProjectOutcome?
    let cancelActivity = try client.start(.export(name: "cancelled", destination: "/opt/new export")) { result in cancelOutcome = result; cancelDone.signal() }
    check(client.cancelActiveActivity(cancelActivity.id), "intentional cancellation targets owned export")
    check(cancelDone.wait(timeout: .now() + .seconds(4)) == .success, "cancelled child reaped")
    if case .unknown = cancelOutcome {} else { check(false, "interrupt does not claim backend rollback") }
    check(!client.cancelActiveActivity(cancelActivity.id), "reaped PID cannot be signalled")
    let beforeTimeout = Date()
    do { _ = try client.query(.list, timeout: 0.15); check(false, "hanging query accepted") } catch {}
    check(Date().timeIntervalSince(beforeTimeout) < 2, "query has caller wall-clock bound")
    usleep(200_000)
    let unknownForAck = try client.recoverActivities().first(where: { $0.projectName == "wrong-name" })!
    let foreignAck = try ProjectClient(executable: cli.path, config: "/opt/foreign config", activityDirectory: activity)
    do { try foreignAck.acknowledgeUnknownActivity(unknownForAck.id); check(false, "foreign configuration acknowledged unknown record") } catch {}
    let foreignDomain = try ProjectClient(executable: cli.path, config: "/opt/config with spaces", domain: "foreign", activityDirectory: activity)
    do { try foreignDomain.acknowledgeUnknownActivity(unknownForAck.id); check(false, "foreign domain acknowledged unknown record") } catch {}
    try client.acknowledgeUnknownActivity(unknownForAck.id)
    check(try client.recoverActivities().contains(where: { $0.id == unknownForAck.id && $0.status == .unknown && $0.acknowledgedAt != nil }), "exact explicit acknowledgement keeps unknown history")
    let liveDone = DispatchSemaphore(value: 0)
    let live = try client.start(.export(name: "live-ack", destination: "/opt/ack fixture")) { _ in liveDone.signal() }
    do { try client.acknowledgeUnknownActivity(live.id); check(false, "live request acknowledged") } catch {}
    do { try client.start(.export(name: "live-ack", destination: "/opt/another fixture"), allowRetryAfterUnknown: true) { _ in }; check(false, "acknowledgement bypassed a live conflict") } catch {}
    check(client.cancelActiveActivity(live.id), "test cleanup interrupts exact owned live export")
    check(liveDone.wait(timeout: .now() + .seconds(4)) == .success, "live ack fixture reaped")
    func chmodACL(_ url: URL, _ spec: String?) throws {
      let process = Process(); process.executableURL = URL(fileURLWithPath: "/bin/chmod")
      process.arguments = spec.map { ["+a", $0, url.path] } ?? ["-N", url.path]
      try process.run(); process.waitUntilExit(); check(process.terminationStatus == 0, "synthetic ACL fixture applied")
    }
    let aclRoot = root.appendingPathComponent("acl-root")
    try FileManager.default.createDirectory(at: aclRoot, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
    try chmodACL(aclRoot, "everyone allow read,search,readattr,readextattr,readsecurity,file_inherit,directory_inherit")
    do { _ = try ProjectClient(executable: cli.path, config: "/opt/config with spaces", activityDirectory: aclRoot); check(false, "granting inherited ACL on private activity root admitted") } catch {}
    try chmodACL(aclRoot, nil)
    let grantingAncestor = root.appendingPathComponent("acl-ancestor")
    try FileManager.default.createDirectory(at: grantingAncestor, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
    try chmodACL(grantingAncestor, "everyone allow read,search,readattr,readextattr,readsecurity,file_inherit,directory_inherit")
    do { _ = try ProjectClient(executable: cli.path, config: "/opt/config with spaces", activityDirectory: grantingAncestor.appendingPathComponent("private")); check(false, "granting ACL ancestry admitted") } catch {}
    try chmodACL(grantingAncestor, nil)
    try chmodACL(aclRoot, "everyone deny delete")
    let denyClient = try ProjectClient(executable: cli.path, config: "/opt/config with spaces", activityDirectory: aclRoot)
    try script("printf '%s\\n' '\(listing)'\n")
    if case .list = try denyClient.query(.list) {} else { check(false, "harmless deny ACL rejected") }
    let denyActivity = try denyClient.recoverActivities().first!
    for name in ["record.json", "events.jsonl"] { try chmodACL(denyActivity.directory.appendingPathComponent(name), "everyone deny delete") }
    check(try denyClient.recoverActivities().contains(where: { $0.id == denyActivity.id }), "deny-only metadata ACL remains readable")
    check(try denyClient.activityEvents(denyActivity.id).count == 1, "deny-only output ACL remains readable")
    try chmodACL(aclRoot.appendingPathComponent(".lock"), "everyone deny delete")
    _ = try ProjectClient(executable: cli.path, config: "/opt/config with spaces", activityDirectory: aclRoot)
    // Clear ACLs only on our disposable fixtures so teardown can remove them.
    for item in try FileManager.default.subpathsOfDirectory(atPath: aclRoot.path) { try chmodACL(aclRoot.appendingPathComponent(item), nil) }
    try chmodACL(aclRoot, nil)
    let metadataStore = try ProjectActivityStore(root: activity)
    let metadataRecord = try metadataStore.records().first(where: { $0.id == unknownForAck.id })!
    for target in [aclRoot.appendingPathComponent(".lock"), activity.appendingPathComponent(".lock"), unknownForAck.directory.appendingPathComponent("record.json"), unknownForAck.directory.appendingPathComponent("events.jsonl")] {
      try chmodACL(target, "everyone allow read,readattr,readextattr,readsecurity")
      if target.lastPathComponent == ".lock" {
        do { _ = try ProjectClient(executable: cli.path, config: "/opt/config with spaces", activityDirectory: target.deletingLastPathComponent()); check(false, "granting ACL on activity lock admitted") } catch {}
      } else if target.lastPathComponent == "record.json" {
        do { _ = try client.recoverActivities(); check(false, "granting ACL on command metadata admitted") } catch {}
        do { try metadataStore.withLock { try metadataStore.save(metadataRecord) }; check(false, "unsafe existing metadata ACL silently replaced") } catch {}
      } else {
        do { _ = try client.activityEvents(unknownForAck.id); check(false, "granting ACL on command output admitted") } catch {}
      }
      try chmodACL(target, nil)
    }
    try script("exec /bin/sleep 60\n")
    let aclDone = DispatchSemaphore(value: 0); var aclOutcome: ProjectOutcome?
    let liveACL = try client.start(.export(name: "live-acl", destination: "/opt/acl fixture")) { aclOutcome = $0; aclDone.signal() }
    let liveOutput = liveACL.directory.appendingPathComponent("events.jsonl")
    try chmodACL(liveOutput, "everyone allow read,readattr,readextattr,readsecurity")
    let aclFinished = aclDone.wait(timeout: .now() + .seconds(3)) == .success
    if !aclFinished { _ = client.cancelActiveActivity(liveACL.id); _ = aclDone.wait(timeout: .now() + .seconds(3)) }
    try chmodACL(liveOutput, nil)
    check(aclFinished, "granting ACL on live output must stop unsafe output and retain unknown outcome")
    if case .unknown = aclOutcome {} else { check(false, "unsafe live output cannot report success") }
    let unsafeRoot = root.appendingPathComponent("linked-activity")
    try FileManager.default.createSymbolicLink(at: unsafeRoot, withDestinationURL: activity)
    do { _ = try ProjectClient(executable: cli.path, config: "/opt/config with spaces", activityDirectory: unsafeRoot); check(false, "symlink activity root accepted") } catch {}
    let unsafeFile = running.directory.appendingPathComponent("events.jsonl")
    try FileManager.default.removeItem(at: unsafeFile)
    try FileManager.default.createSymbolicLink(at: unsafeFile, withDestinationURL: cli)
    do { _ = try secondClient.activityEvents(running.id); check(false, "symlink retained events accepted") } catch {}
    print("PASS: typed argv/environment, progress, error/unknown classification, duplicate gates, detached recovery, bounded and unsafe output")
  }
}
