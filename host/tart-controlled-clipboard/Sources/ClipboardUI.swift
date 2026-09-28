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
  let preparedDisplay: VZVirtualMachineView
  @StateObject private var controller: ClipboardWindowController
  init(vm: VM, preparedDisplay: VZVirtualMachineView, target: ClipboardTarget?) {
    self.vm = vm
    self.preparedDisplay = preparedDisplay
    _controller = StateObject(wrappedValue: ClipboardWindowController(target: target, invoker: ClipboardProcessInvoker()))
  }
  var body: some View {
    VStack(spacing: 0) {
      ClipboardStrip(controller: controller)
      Divider()
      PreparedVirtualMachineView(view: preparedDisplay)
    }
    .background(ClipboardWindowBinding(controller: controller))
    .focusedSceneValue(\.clipboardController, controller)
    .onReceive(vm.virtualMachine.publisher(for: \.state, options: [.initial, .new])) { state in
      controller.running = state == .running
    }
    .onDisappear { controller.close() }
  }
}

// SwiftUI must mount the exact view bound before VM startup. Creating a
// replacement in makeNSView would discard that early Virtualization binding.
struct PreparedVirtualMachineView: NSViewRepresentable {
  let view: VZVirtualMachineView
  func makeNSView(context: Context) -> VZVirtualMachineView { view }
  func updateNSView(_ view: VZVirtualMachineView, context: Context) {}
}
// Keep the host-owned transfer affordance to one line above the guest display.
// Full target identity and the no-sync explanation remain available as tooltips.
struct ClipboardStrip: View {
  @ObservedObject var controller: ClipboardWindowController
  var body: some View {
    HStack(spacing: 8) {
      Text("Clipboard copy:").font(.caption)
      Text(controller.target.map { "\($0.domain)/\($0.session)" } ?? "unavailable")
        .font(.caption).lineLimit(1).truncationMode(.middle)
        .help(controller.target.map { "Target sandbox: \($0.domain)/\($0.session) (\($0.sessionID))" } ?? "Target sandbox: unavailable")
      Spacer(minLength: 4)
      Button("GUEST -> HOST") {
        let captured = controller
        captured.perform(.pull)
      }.disabled(!controller.available).fixedSize(horizontal: true, vertical: false)
      Button("HOST -> GUEST") {
        let captured = controller
        captured.perform(.push)
      }.disabled(!controller.available).fixedSize(horizontal: true, vertical: false)
      Text(controller.status).font(.caption).lineLimit(1).truncationMode(.tail)
        .frame(maxWidth: 180, alignment: .leading).help(controller.status)
    }
    .controlSize(.small)
    .padding(.horizontal, 8).padding(.vertical, 4)
    .frame(maxWidth: .infinity, alignment: .leading)
    .background(Color(nsColor: .windowBackgroundColor))
    .help("Copy text between the host and guest clipboards. Nothing transfers automatically.")
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
