// Compile-only VM dependency stand-ins; never instantiate or run a VM.
import SwiftUI
import Virtualization
final class VM: ObservableObject {
  var virtualMachine: VZVirtualMachine { fatalError("compile-only") }
}
struct VMView: View {
  let vm: VM
  let capturesSystemKeys: Bool
  var body: some View { EmptyView() }
}
