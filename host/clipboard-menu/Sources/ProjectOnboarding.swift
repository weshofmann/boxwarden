import AppKit

extension ProjectWindowController {
  @objc func showSetupHelp(_ sender: Any?) {
    let alert = NSAlert()
    let updateNeeded = setupInspection?.status == "project_setup_invalid" || setupInspection?.recipePreparationAvailable == false && setupInspection?.status == "ready"
    alert.messageText = updateNeeded ? "Update the saved project assets explicitly" : "Set up Boxwarden on this Mac"
    alert.informativeText = "Choose a private Boxwarden JSON configuration outside the workspace backing filesystem. Connect its configured storage first.\n\nOn a new Mac, host tool initialization needs your attended authentication. Use the bundled quickstart for that explicit step; this app does not install or repair privileged tools.\n\nOnce the configuration and host tools are admitted, Prepare Project Assets selects the Ubuntu installer, checker package and existing build tools. No provider sign-in is required."
    if updateNeeded { alert.informativeText = "The existing setup remains selected. Recipe creation and system replacement need version-2 recipe assets. Use the bundled upgrade instructions to perform an explicit setup update; existing asset locators are not silently replaced. Existing projects remain discoverable." }
    alert.addButton(withTitle: updateNeeded ? "Open Upgrade Instructions" : "Open Bundled Quickstart"); alert.addButton(withTitle: "Close")
    if alert.runModal() == .alertFirstButtonReturn {
      let package = Bundle.main.bundleURL.deletingLastPathComponent()
      NSWorkspace.shared.open(package.appendingPathComponent(updateNeeded ? "UPGRADING.md" : "TRY-ME.md"))
    }
  }
  @objc func prepareProjectAssets(_ sender: Any?) {
    guard !closing, !switchingConfiguration, !hasActiveOperation, let client,
          setupInspection?.selectionAcceptable == true, window?.attachedSheet == nil else { return }
    let package = Bundle.main.bundleURL.deletingLastPathComponent().path
    let fields = ["Ubuntu ARM64 desktop ISO", "ARM64 e2fsck checker package", "Go executable", "zstd executable", "OpenSSL executable", "xorriso executable"]
    let saved = defaults.stringArray(forKey: "ValidatedPreparationInputs") ?? []
    let sheet = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 640, height: 450), styleMask: [.titled], backing: .buffered, defer: false)
    sheet.title = "Prepare Project Assets"
    let root = NSStackView(); root.orientation = .vertical; root.alignment = .leading; root.spacing = 10
    root.translatesAutoresizingMaskIntoConstraints = false
    let explanation = NSTextField(wrappingLabelWithString: "Select existing files and tools. Preparation builds managed project helpers using this package; it can take several minutes. Host tools are checked first. Existing setup is never silently replaced.")
    root.addArrangedSubview(explanation)
    var inputs: [NSTextField] = []
    for (index, label) in fields.enumerated() {
      let field = NSTextField(string: saved.count == 6 ? saved[index] : "")
      field.placeholderString = label; field.setAccessibilityLabel(label)
      let pick = NSButton(title: "Choose…", target: nil, action: nil)
      let row = NSStackView(views: [field, pick]); row.spacing = 8
      row.widthAnchor.constraint(equalToConstant: 600).isActive = true
      field.widthAnchor.constraint(greaterThanOrEqualToConstant: 480).isActive = true
      pick.target = self
      // Buttons carry only this sheet's field through their menu action closure.
      let picker = PreparationPicker(field: field, parent: sheet)
      preparationPickers.append(picker); pick.target = picker; pick.action = #selector(PreparationPicker.choose(_:))
      root.addArrangedSubview(row); inputs.append(field)
    }
    let actions = NSStackView(); actions.spacing = 8
    let cancel = NSButton(title: "Cancel", target: nil, action: nil)
    let prepare = NSButton(title: "Prepare Assets", target: nil, action: nil)
    let completion = PreparationCompletion { [weak self] accepted in
      guard let self else { return }
      let paths = inputs.map { $0.stringValue }
      if accepted && !paths.allSatisfy(ProjectCommand.validPath) {
        let a = NSAlert(); a.messageText = "Choose all six files and tools"; a.informativeText = "Each selection must be an absolute path."; a.beginSheetModal(for: sheet); return
      }
      self.window?.endSheet(sheet); self.sheetController = nil; self.preparationPickers.removeAll(); self.preparationCompletion = nil
      if accepted { self.startPreparation(client: client, inputs: [package] + paths) }
    }
    preparationCompletion = completion
    cancel.target = completion; cancel.action = #selector(PreparationCompletion.cancel(_:))
    prepare.target = completion; prepare.action = #selector(PreparationCompletion.prepare(_:)); prepare.keyEquivalent = "\r"
    actions.addArrangedSubview(cancel); actions.addArrangedSubview(prepare); root.addArrangedSubview(actions)
    sheet.contentView!.addSubview(root)
    NSLayoutConstraint.activate([root.leadingAnchor.constraint(equalTo: sheet.contentView!.leadingAnchor, constant: 20), root.trailingAnchor.constraint(equalTo: sheet.contentView!.trailingAnchor, constant: -20), root.topAnchor.constraint(equalTo: sheet.contentView!.topAnchor, constant: 20)])
    explanation.widthAnchor.constraint(equalTo: root.widthAnchor).isActive = true
    sheetController = NSWindowController(window: sheet)
    window?.beginSheet(sheet)
    sheet.makeFirstResponder(inputs.first)
  }
  private func startPreparation(client: ProjectClient, inputs: [String]) {
    guard let ticket = presentation.beginOperation() else { return }
    client.cancelQueries(); clipboardClient?.cancelQueries()
    report("Preparing project assets… The CLI will report actual progress.")
    do {
      activity = try client.start(.setupPrepare(inputs: inputs), progress: { [weak self] event in
        DispatchQueue.main.async { if let message = event.message { self?.report(message) } }
      }, completion: { [weak self] outcome in
        DispatchQueue.main.async {
          guard let self else { return }
          self.presentation.finishOperation(ticket)
          if case .success(.setup(let inspection)) = outcome {
            self.setupInspection = inspection
            if inspection.status == "ready" { self.defaults.set(Array(inputs.dropFirst()), forKey: "ValidatedPreparationInputs") }
          }
          self.report(outcome.message); try? self.recoverActivity(); self.refreshProjects(nil)
        }
      })
    } catch { presentation.finishOperation(ticket); report(error.localizedDescription) }
    render()
  }
}

final class PreparationPicker: NSObject {
  let field: NSTextField; weak var parent: NSWindow?
  init(field: NSTextField, parent: NSWindow) { self.field = field; self.parent = parent }
  @objc func choose(_ sender: Any?) {
    guard let parent else { return }
    let panel = NSOpenPanel(); panel.canChooseDirectories = false; panel.canChooseFiles = true; panel.allowsMultipleSelection = false
    panel.beginSheetModal(for: parent) { [weak self] response in if response == .OK, let path = panel.url?.path { self?.field.stringValue = path } }
  }
}
final class PreparationCompletion: NSObject {
  let completion: (Bool) -> Void
  init(_ completion: @escaping (Bool) -> Void) { self.completion = completion }
  @objc func prepare(_ sender: Any?) { completion(true) }
  @objc func cancel(_ sender: Any?) { completion(false) }
}
