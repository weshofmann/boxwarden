import Foundation
import Darwin

@main struct ProjectFirstRunTests {
  static func check(_ value: @autoclosure () throws -> Bool, _ message: String) {
    guard (try! value()) else { fputs("FAIL: \(message)\n", stderr); exit(1) }
  }
  static func main() throws {
    let root = URL(fileURLWithPath: "/private/tmp").appendingPathComponent("bw-first-run-" + UUID().uuidString)
    try FileManager.default.createDirectory(at: root, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
    defer { try? FileManager.default.removeItem(at: root) }
    let cli = root.appendingPathComponent("cli")
    let input = NativeFirstRunInput(setupID: "11111111-1111-4111-8111-111111111111", dataLocation: "/data with spaces", packageRoot: "/package", isoPath: "/verified.iso")
    let digest = String(repeating: "a", count: 64)
    let config = "/private/config.json"
    let fields: [String: Any] = ["version": 1, "scope": "alpha_first_run", "setup_id": input.setupID,
      "data_location": input.dataLocation, "package_root": input.packageRoot, "iso_path": input.isoPath,
      "config_path": config, "state_root": "/data with spaces/new-state", "mount_point": "/Volumes/Data", "volume_uuid": "volume",
      "available_bytes": 1000, "reserve_bytes": 100, "host_status": "ready", "status": "ready", "guidance": "Review setup",
      "expected_digest": digest, "existing_entries": 0, "alternatives": [], "prerequisites": [], "next_actions": ["create_setup"]]
    func script(_ body: String) throws {
      try Data(("#!/bin/sh\n" + body).utf8).write(to: cli)
      try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: cli.path)
    }
    func encoded(_ value: [String: Any]) throws -> String { String(data: try JSONSerialization.data(withJSONObject: value, options: .sortedKeys), encoding: .utf8)! }
    let prepared = try ProjectCommand.setupPreparePackaged(package: input.packageRoot, iso: input.isoPath).arguments(config: config, domain: "alpha")
    check(prepared == ["--config", config, "setup", "prepare", "--json", "--package", input.packageRoot, "--iso", input.isoPath, "--prebuilt"], "explicit packaged retry needs no compiler tool-path selections")
    let planCommand = ProjectCommand.setupPlan(input)
    let planArgs = try planCommand.arguments(config: "/unselected.json", domain: "alpha")
    check(planArgs.prefix(3) == ["setup", "plan", "--json"] && !planArgs.contains("--config") && !planArgs.contains("--domain") && !planCommand.isMutation, "first-run planning has no selected config or mutation authority")
    let empty = NativeFirstRunInput(setupID: input.setupID, dataLocation: "", packageRoot: input.packageRoot, isoPath: "")
    check(try ProjectCommand.setupPlan(empty).arguments(config: "/unselected.json", domain: "alpha").contains(""), "read-only prerequisite plan permits missing selections")
    do { _ = try ProjectCommand.setupCreate(empty, digest: digest).arguments(config: config, domain: "alpha"); check(false, "create permits empty selections") } catch {}
    let client = try ProjectClient(executable: cli.path, config: "/unselected.json", activityDirectory: root.appendingPathComponent("queries"))
    let checks = planArgs.enumerated().map { "[ \"${\($0.offset + 1)}\" = '\($0.element)' ] || exit 31" }.joined(separator: "\n")
    try script(checks + "\nprintf '%s\\n' '\(try encoded(fields))'\n")
    guard case .firstRunPlan(let plan) = try client.query(planCommand) else { fatalError("missing plan") }
    check(plan.canCreate && plan.configPath == config, "exact plain JSON plan accepted")
    var blocked = fields; blocked["status"] = "insufficient_space"; blocked["expected_digest"] = ""
    blocked["available_bytes"] = 24076132352; blocked["reserve_bytes"] = 30434484224
    try script("printf '%s\\n' '\(try encoded(blocked))'\n")
    guard case .firstRunPlan(let capacity) = try client.query(planCommand) else { fatalError("missing capacity plan") }
    check(!capacity.canCreate && capacity.explanation.contains("22.42 GiB") && capacity.explanation.contains("28.34 GiB"), "blocked setup shows actual available and required capacity without integer truncation")
    for key in ["setup_id", "data_location", "package_root", "iso_path"] {
      var changed = fields; changed[key] = key == "setup_id" ? "22222222-2222-4222-8222-222222222222" : "/different"
      try script("printf '%s\\n' '\(try encoded(changed))'\n")
      do { _ = try client.query(planCommand); check(false, "mismatched \(key) accepted") } catch {}
    }
    let creator = try ProjectClient(executable: cli.path, config: config, activityDirectory: root.appendingPathComponent("create"))
    let command = ProjectCommand.setupCreate(plan.input, digest: plan.expectedDigest)
    let args = try command.arguments(config: config, domain: "alpha")
    check(command.isMutation && !args.contains("--config") && args.suffix(2) == ["--expected-digest", digest], "creation pins the reviewed plan digest")
    let inspection: [String: Any] = ["version": 1, "scope": "alpha_project_setup", "status": "ready", "config_path": config, "config_valid": true, "selection_acceptable": true, "guidance": "Ready", "next_actions": []]
    let terminal: [String: Any] = ["version": 1, "type": "result", "operation": "setup.create", "data": inspection]
    try script("printf '%s\\n' '\(try encoded(terminal))'\n")
    let done = DispatchSemaphore(value: 0); var outcome: ProjectOutcome?
    try creator.start(command) { outcome = $0; done.signal() }
    check(done.wait(timeout: .now() + 5) == .success, "setup mutation reaped")
    if case .success(.setup(let result)) = outcome { check(result.configPath == config, "creation binds candidate config") } else { check(false, "setup create response") }
    let activities = try creator.recoverActivities()
    check(activities.contains { $0.operation == "setup.create" && $0.status == .succeeded }, "setup creation uses durable mutation receipt")
    print("PASS: first-run plan binding, prerequisite query, reviewed digest, candidate configuration and mutation receipt")
  }
}
