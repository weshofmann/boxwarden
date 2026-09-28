import Foundation

@main struct CLIClientTests {
  static func main() {
    func check(_ condition: @autoclosure () -> Bool, _ message: String) {
      if !condition() { fputs("FAIL: \(message)\n", stderr); exit(1) }
    }
    let folder = FileManager.default.temporaryDirectory.appendingPathComponent("bw-menu-test-" + UUID().uuidString)
    try! FileManager.default.createDirectory(at: folder, withIntermediateDirectories: false)
    defer { try? FileManager.default.removeItem(at: folder) }
    let cli = folder.appendingPathComponent("synthetic-cli")
    let script = """
    #!/bin/sh
    [ -z "$BOXWARDEN_MENU_SECRET" ] || exit 31
    [ "$1" = --config ] && [ "$2" = '/opt/config with spaces' ] && [ "$3" = --domain ] && [ "$4" = work ] || exit 32
    if [ "$5" = clipboard ] && [ "$6" = targets ]; then
      printf '%s\\n' '{"targets":[{"domain":"work","session":"dev","session_id":"11111111-1111-4111-8111-111111111111","backend_kind":"tart","backend_object":"bw-dev","generation":"22222222-2222-4222-8222-222222222222","available":true}]}'
      exit 0
    fi
    [ "$5" = clipboard ] && [ "$6" = push ] && [ "$7" = dev ] && [ "$8" = --expected-session-id ] && [ "$9" = 11111111-1111-4111-8111-111111111111 ] || exit 33
    printf 'sensitive synthetic payload\\n'
    exit 0
    """
    try! Data(script.utf8).write(to: cli)
    try! FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: cli.path)
    setenv("BOXWARDEN_MENU_SECRET", "ambient", 1)
    defer { unsetenv("BOXWARDEN_MENU_SECRET") }
    let client = CLIClient(executable: cli.path, config: "/opt/config with spaces")
    let targets = try! client.discover(domain: "work")
    check(targets.count == 1 && targets[0].available && targets[0].session == "dev", "structured exact-domain query with closed environment")
    let state = MenuState()
    let refresh = state.beginRefresh(); state.acceptRefresh(refresh, targets: targets)
    state.select(targets[0])
    let request = state.beginTransfer(.push)!
    let done = DispatchSemaphore(value: 0)
    var result: TransferResult = .failed
    let child = client.transfer(request) { outcome in result = outcome; done.signal() }
    check(child != nil, "transfer process started")
    check(done.wait(timeout: .now() + .seconds(5)) == .success && result == .success, "transfer uses exact vector with no ambient secret")
    try! Data("#!/bin/sh\ntrap '' TERM\nexec /bin/sleep 60\n".utf8).write(to: cli)
    let hanging = CLIClient(executable: cli.path, config: "/opt/config with spaces", deadlineSeconds: 0.2)
    let cancelDone = DispatchSemaphore(value: 0)
    var cancelled: TransferResult = .failed
    let owned = hanging.transfer(request) { outcome in cancelled = outcome; cancelDone.signal() }
    check(owned != nil, "hanging child started")
    owned?.cancel()
    check(cancelDone.wait(timeout: .now() + .seconds(4)) == .success && cancelled == .cancelled, "uncooperative child is killed and reaped after cancellation")
    let started = Date()
    do { _ = try hanging.discover(domain: "work"); check(false, "hung query accepted") } catch {}
    check(Date().timeIntervalSince(started) < 4, "hung discovery has a wall-clock bound")
    let shutdownClient = CLIClient(executable: cli.path, config: "/opt/config with spaces")
    let stopped = DispatchSemaphore(value: 0)
    let reaped = DispatchSemaphore(value: 0)
    _ = shutdownClient.transfer(request) { _ in reaped.signal() }
    shutdownClient.shutdown { stopped.signal() }
    check(stopped.wait(timeout: .now() + .seconds(4)) == .success && reaped.wait(timeout: .now()) == .success,
          "quit cancels and reaps owned transfer child")
    let partialScript = """
    #!/bin/sh
    [ "$4" = personal ] && exit 5
    [ "$4" = work ] || exit 6
    printf '%s\\n' '{"targets":[{"domain":"work","session":"dev","session_id":"11111111-1111-4111-8111-111111111111","backend_kind":"tart","backend_object":"bw-dev","generation":"22222222-2222-4222-8222-222222222222","available":true}]}'
    """
    try! Data(partialScript.utf8).write(to: cli)
    let configured = Data(#"{"domains":{"personal":{},"work":{}}}"#.utf8)
    let partial = try! client.discoverConfiguredDomains(from: configured)
    check(partial.failedDomains == 1 && partial.targets.count == 1 && partial.targets[0].domain == "work",
          "one failed configured domain leaves successful explicit-domain targets visible")
    print("PASS: direct CLI query and transfer use fixed argv and closed environment")
  }
}
