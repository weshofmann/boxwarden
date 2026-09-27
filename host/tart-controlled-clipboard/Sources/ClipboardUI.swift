// Original Boxwarden addition; retains pinned upstream VMView unchanged.
import Cocoa
import SwiftUI
import Virtualization

private struct ClipboardControllerKey: FocusedValueKey {
  typealias Value = ClipboardWindowController
}
extension FocusedValues {
  var clipboardController: ClipboardWindowController? {
    get { self[ClipboardControllerKey.self] }
    set { self[ClipboardControllerKey.self] = newValue }
  }
}
struct ControlledClipboardCommands: Commands {
  @FocusedValue(\.clipboardController) private var focused
  var body: some Commands {
    CommandMenu("Clipboard") {
      if let captured = focused {
        ControlledClipboardMenu(controller: captured)
      } else {
        Button("Copy Host Clipboard to Guest", action: {}).disabled(true)
        Button("Copy Guest Clipboard to Host", action: {}).disabled(true)
      }
    }
  }
}
private struct ControlledClipboardMenu: View {
  @ObservedObject var controller: ClipboardWindowController
  var body: some View {
    Button("Copy Host Clipboard to Guest") {
      let captured = controller
      captured.perform(.push)
    }.disabled(!controller.available)
    Button("Copy Guest Clipboard to Host") {
      let captured = controller
      captured.perform(.pull)
    }.disabled(!controller.available)
  }
}
struct ControlledClipboardVMView: View {
  @ObservedObject var vm: VM
  let capturesSystemKeys: Bool
  @StateObject private var controller: ClipboardWindowController
  init(vm: VM, capturesSystemKeys: Bool, target: ClipboardTarget?) {
    self.vm = vm
    self.capturesSystemKeys = capturesSystemKeys
    _controller = StateObject(wrappedValue: ClipboardWindowController(target: target, invoker: ClipboardProcessInvoker()))
  }
  var body: some View {
    VStack(spacing: 0) {
      VStack(alignment: .leading, spacing: 4) {
        Text("Copy text between the host and guest clipboards.")
        Text("Nothing transfers automatically.")
        Text(controller.target.map { "Target sandbox: \($0.domain)/\($0.session) (\($0.sessionID))" } ?? "Target sandbox: unavailable")
          .font(.caption).textSelection(.disabled)
        HStack {
          Button("HOST -> GUEST") {
            let captured = controller
            captured.perform(.push)
          }.disabled(!controller.available)
          Button("GUEST -> HOST") {
            let captured = controller
            captured.perform(.pull)
          }.disabled(!controller.available)
          Text(controller.status).font(.caption)
        }
      }.padding(8).frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(nsColor: .windowBackgroundColor))
      Divider()
      VMView(vm: vm, capturesSystemKeys: capturesSystemKeys)
    }
    .background(ClipboardWindowBinding(controller: controller))
    .focusedSceneValue(\.clipboardController, controller)
    .onReceive(vm.virtualMachine.publisher(for: \.state, options: [.initial, .new])) { state in
      controller.running = state == .running
    }
    .onDisappear { controller.close() }
  }
}
// Key-window eligibility is checked separately from SwiftUI focus: auxiliary
// windows cannot accidentally invoke a stale previously focused VM scene.
private struct ClipboardWindowBinding: NSViewRepresentable {
  let controller: ClipboardWindowController
  func makeNSView(context: Context) -> BindingView { BindingView(controller: controller) }
  func updateNSView(_ view: BindingView, context: Context) {}
  final class BindingView: NSView {
    let controller: ClipboardWindowController
    private var observers: [NSObjectProtocol] = []
    init(controller: ClipboardWindowController) { self.controller = controller; super.init(frame: .zero) }
    required init?(coder: NSCoder) { fatalError("not supported") }
    override func viewDidMoveToWindow() {
      super.viewDidMoveToWindow()
      observers.forEach(NotificationCenter.default.removeObserver)
      observers.removeAll()
      controller.keyWindow = window?.isKeyWindow == true
      guard let window = window else { controller.close(); return }
      for event in [NSWindow.didBecomeKeyNotification, NSWindow.didResignKeyNotification, NSWindow.willCloseNotification] {
        observers.append(NotificationCenter.default.addObserver(forName: event, object: window, queue: .main) { [weak self, weak window] notification in
          guard let self = self else { return }
          MainActor.assumeIsolated {
            if notification.name == NSWindow.willCloseNotification { self.controller.close() }
            self.controller.keyWindow = window?.isKeyWindow == true
          }
        })
      }
    }
    deinit { observers.forEach(NotificationCenter.default.removeObserver) }
  }
}
