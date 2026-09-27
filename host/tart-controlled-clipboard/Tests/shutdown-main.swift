import Foundation
import Darwin

@main struct ShutdownTest {
  @MainActor static func main() async {
    let temporary = FileManager.default.temporaryDirectory.appendingPathComponent("bw-shutdown-" + UUID().uuidString)
    try! FileManager.default.createDirectory(at: temporary, withIntermediateDirectories: false)
    defer { try? FileManager.default.removeItem(at: temporary) }
    let executable = temporary.appendingPathComponent("child")
    let marker = temporary.appendingPathComponent("pid")
    try! Data("#!/bin/sh\ntrap '' TERM\nprintf '%s' $$ > '\(marker.path)'\nexec /bin/sleep 60\n".utf8).write(to: executable)
    try! FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: executable.path)
    let target = ClipboardTarget(cli: executable.path, config: "/opt/config", domain: "personal", session: "test", sessionID: "11111111-1111-4111-8111-111111111111", backendObject: "bw-test", generation: "22222222-2222-4222-8222-222222222222")
    let controller = ClipboardWindowController(target: target, invoker: ClipboardProcessInvoker())
    controller.running = true; controller.keyWindow = true; controller.perform(.push)
    for _ in 0..<200 {
      if FileManager.default.fileExists(atPath: marker.path) { break }
      try? await Task.sleep(nanoseconds: 10_000_000)
    }
    guard let pid = Int32(try! String(contentsOf: marker, encoding: .utf8)) else { fatalError("fixture did not start") }
    await ClipboardProcessLifecycle.shared.shutdown()
    guard kill(pid, 0) == -1 && errno == ESRCH else {
      kill(pid, SIGKILL)
      print("FAIL: normal parent shutdown returned before TERM-resistant child was killed and reaped")
      exit(1)
    }
    controller.perform(.pull)
    try? await Task.sleep(nanoseconds: 100_000_000)
    guard !controller.busy else { print("FAIL: shutdown must reject future transfer admission"); exit(1) }
    print("PASS: normal shutdown drains/reaps own resistant child and closes admission")
  }
}
