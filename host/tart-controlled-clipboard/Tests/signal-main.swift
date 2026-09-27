import Foundation
import Darwin

@main struct SignalFixture {
  @MainActor static func main() async {
    guard CommandLine.arguments.count == 2 else { exit(2) }
    let sources = ClipboardProcessLifecycle.shared.installStopSignals { exit(0) }
    defer { sources.forEach { $0.cancel() } }
    let target = ClipboardTarget(cli: CommandLine.arguments[1], config: "/opt/config", domain: "personal", session: "signal-fixture", sessionID: "11111111-1111-4111-8111-111111111111", backendObject: "bw-signal-fixture", generation: "22222222-2222-4222-8222-222222222222")
    let controller = ClipboardWindowController(target: target, invoker: ClipboardProcessInvoker())
    controller.running = true; controller.keyWindow = true
    controller.perform(.push)
    while true { try? await Task.sleep(nanoseconds: 100_000_000) }
  }
}
