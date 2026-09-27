import Foundation
@MainActor final class HeldChild: ClipboardChild {
 var cancelled = false
 func cancel() { cancelled = true }
}
@MainActor final class HeldInvoker: ClipboardInvoking {
 var calls: [(ClipboardTarget, ClipboardDirection)] = []
 var completions: [@MainActor (ClipboardResult) -> Void] = []
 var children: [HeldChild] = []
 func start(target: ClipboardTarget, direction: ClipboardDirection, completion: @escaping @MainActor (ClipboardResult) -> Void) -> ClipboardChild {
  calls.append((target, direction)); completions.append(completion)
  let child = HeldChild(); children.append(child); return child
 }
}
@main struct Tests {
 @MainActor static func main() async {
  func check(_ value: @autoclosure () -> Bool, _ message: String) {
   guard value() else { print("FAIL: \(message)"); exit(1) }
  }
  let first = ClipboardTarget(cli: "/opt/boxwarden", config: "/opt/config", domain: "personal", session: "alpha", sessionID: "11111111-1111-4111-8111-111111111111", backendObject: "bw-alpha", generation: "22222222-2222-4222-8222-222222222222")
  let invoker = HeldInvoker()
  let controller = ClipboardWindowController(target: first, invoker: invoker)
  controller.running = true; controller.keyWindow = true
  check(controller.available, "eligible target must enable transfer")
  controller.perform(.push)
  check(controller.busy && !controller.available, "admission must synchronously become busy")
  controller.perform(.pull)
  check(invoker.calls.count == 1, "busy operation must reject without queued transfer")
  controller.keyWindow = false
  let other = ClipboardWindowController(target: nil, invoker: invoker)
  other.keyWindow = true; other.running = true; other.perform(.pull)
  check(invoker.calls.count == 1 && invoker.calls[0].0 == first, "focus changes must not retarget flight or enable missing target")
  invoker.completions[0](.success)
  check(!controller.busy && controller.status == "Transfer completed.", "completion must publish payload-free success")
  controller.keyWindow = true; controller.perform(.pull)
  check(invoker.calls.count == 2 && invoker.calls[1].1 == .pull, "menu and button shared action supports both directions")
  controller.close()
  check(invoker.children[1].cancelled && !controller.available, "closed target cancels own child")
  invoker.completions[1](.success)
  check(controller.status != "Transfer completed.", "late completion cannot restore closed eligibility")
  check(first.arguments(.push) == ["--config", "/opt/config", "--domain", "personal", "clipboard", "push", "alpha", "--expected-session-id", "11111111-1111-4111-8111-111111111111", "--expected-backend-kind", "tart", "--expected-backend-object", "bw-alpha", "--expected-generation", "22222222-2222-4222-8222-222222222222"], "argv carries exact binding and element boundaries")
  let stopped = ClipboardWindowController(target: first, invoker: invoker)
  stopped.keyWindow = true
  stopped.perform(.push)
  check(invoker.calls.count == 2, "stopped target cannot start")
  stopped.running = true; stopped.perform(.push)
  stopped.running = false
  check(invoker.children[2].cancelled && !stopped.available, "lost running target cancels own flight")
  invoker.completions[2](.cancelled)
  check(!stopped.busy && stopped.status.contains("may have changed"), "cancel does not promise rollback")
  let absent = try! ClipboardTarget.validated(cli: nil, config: nil, domain: nil, session: nil, sessionID: nil, backendObject: "bw-alpha", generation: nil, noClipboard: false)
  check(absent == nil, "ordinary Tart launch has no transfer authority")
  for (cli, config, domain, sessionID, noClipboard) in [("relative", "/opt/config", "personal", first.sessionID, true), (first.cli, "/opt/../config", "personal", first.sessionID, true), (first.cli, first.config, "work\nsecret", first.sessionID, true), (first.cli, first.config, first.domain, "invalid", true), (first.cli, first.config, first.domain, first.sessionID, false)] {
    do {
      _ = try ClipboardTarget.validated(cli: cli, config: config, domain: domain, session: first.session, sessionID: sessionID, backendObject: first.backendObject, generation: first.generation, noClipboard: noClipboard)
      check(false, "unsafe/incomplete metadata cannot grant authority")
    } catch {}
  }
  do {
    _ = try ClipboardTarget.validated(cli: first.cli, config: nil, domain: first.domain, session: first.session, sessionID: first.sessionID, backendObject: first.backendObject, generation: first.generation, noClipboard: true)
    check(false, "partial metadata cannot grant authority")
  } catch {}
  // A real synthetic subprocess checks output discard, closed environment and reap.
  // It never reads/writes the real clipboard and does not launch a guest.
  let temporary = FileManager.default.temporaryDirectory.appendingPathComponent("bw-ui-test-" + UUID().uuidString)
  try! FileManager.default.createDirectory(at: temporary, withIntermediateDirectories: false)
  defer { try? FileManager.default.removeItem(at: temporary) }
  let executable = temporary.appendingPathComponent("synthetic-cli")
  try! Data("#!/bin/sh\n[ -z \"$BOXWARDEN_UI_TEST_SECRET\" ] || exit 9\n[ \"$1\" = --config ] || exit 8\n[ \"$2\" = '/opt/config with spaces' ] || exit 7\n/usr/bin/printf 'untrusted diagnostic\\n' >&2\nexit 0\n".utf8).write(to: executable)
  try! FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: executable.path)
  setenv("BOXWARDEN_UI_TEST_SECRET", "synthetic", 1)
  defer { unsetenv("BOXWARDEN_UI_TEST_SECRET") }
  let synthetic = ClipboardTarget(cli: executable.path, config: "/opt/config with spaces", domain: first.domain, session: first.session, sessionID: first.sessionID, backendObject: first.backendObject, generation: first.generation)
  let real = ClipboardWindowController(target: synthetic, invoker: ClipboardProcessInvoker())
  real.running = true; real.keyWindow = true; real.perform(.push)
  for _ in 0..<200 { if !real.busy { break }; try? await Task.sleep(nanoseconds: 10_000_000) }
  check(!real.busy && real.status == "Transfer completed.", "real child preserves argv, drops ambient environment and discards diagnostics")
  try! Data("#!/bin/sh\ntrap '' TERM\nexec /bin/sleep 60\n".utf8).write(to: executable)
  real.perform(.pull)
  try? await Task.sleep(nanoseconds: 100_000_000)
  real.running = false
  for _ in 0..<300 { if !real.busy { break }; try? await Task.sleep(nanoseconds: 10_000_000) }
  check(!real.busy && real.status.contains("cancelled"), "uncooperative own child is killed and reaped after bounded cancellation")
  print("PASS: clipboard viewer controller, validation and real synthetic child behavior")
 }
}
