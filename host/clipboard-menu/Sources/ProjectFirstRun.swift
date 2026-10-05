import AppKit

extension ProjectWindowController {
  var firstRunPackageRoot: String {
    if let resource = Bundle.main.resourceURL?.appendingPathComponent("Boxwarden"), FileManager.default.fileExists(atPath: resource.path) { return resource.path }
    return Bundle.main.bundleURL.deletingLastPathComponent().path
  }
  @objc func setUpBoxwarden(_ sender: Any?) {
    guard !closing, !switchingConfiguration, !hasActiveOperation, let window, window.attachedSheet == nil else { return }
    let setupID = UUID().uuidString.lowercased()
    let choice = UUID(); firstRunChoice = choice
    let placeholder = FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent("Library/Application Support/org.boxwarden.project-manager/UnselectedConfiguration.json").path
    if let previous = firstRunClient { retireFirstRunQueries(previous) }
    do { firstRunClient = try ProjectClient(executable: executable, config: placeholder, activityDirectory: activityDirectory) }
    catch { report(error.localizedDescription); return }
    let sheet = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 680, height: 430), styleMask: [.titled], backing: .buffered, defer: false)
    sheet.title = "Set up Boxwarden"
    let root = stack([], vertical: true); root.spacing = 12; root.translatesAutoresizingMaskIntoConstraints = false
    let explanation = NSTextField(wrappingLabelWithString: "Choose where Boxwarden will keep its private data and a verified Ubuntu ARM64 desktop installer. Boxwarden checks packaged resources and existing host tools automatically. Review the exact locations before anything is created.")
    explanation.widthAnchor.constraint(equalToConstant: 630).isActive = true
    root.addArrangedSubview(explanation)
    let data = NSTextField(string: ""), iso = NSTextField(string: "")
    data.setAccessibilityIdentifier("Data location"); iso.setAccessibilityIdentifier("Ubuntu installer")
    let available = NSPopUpButton(); available.addItem(withTitle: "Available private data locations"); available.isEnabled = false
    let details = NSTextView(frame: NSRect(x: 0, y: 0, width: 630, height: 110))
    details.isEditable = false; details.isSelectable = true; details.isVerticallyResizable = true
    details.font = .systemFont(ofSize: 12); details.textColor = .secondaryLabelColor
    details.string = "Checking prerequisites…"; details.textContainer?.widthTracksTextView = true
    let detailScroll = NSScrollView(); detailScroll.documentView = details; detailScroll.hasVerticalScroller = true
    detailScroll.widthAnchor.constraint(equalToConstant: 630).isActive = true
    detailScroll.heightAnchor.constraint(equalToConstant: 110).isActive = true
    var dataLocations: [String] = []
    let dataChoice = FirstRunSelection { index in if dataLocations.indices.contains(index - 1) { data.stringValue = dataLocations[index - 1] } }
    available.target = dataChoice; available.action = #selector(FirstRunSelection.choose(_:))
    // Keep all targets alive until this sheet is closed.
    let dataPicker = FirstRunPicker(field: data, parent: sheet, directory: true)
    let isoPicker = FirstRunPicker(field: iso, parent: sheet, directory: false)
    for (title, field, picker) in [("Data location", data, dataPicker), ("Ubuntu ARM64 desktop ISO", iso, isoPicker)] {
      let button = NSButton(title: "Choose…", target: picker, action: #selector(FirstRunPicker.choose(_:)))
      field.widthAnchor.constraint(equalToConstant: 520).isActive = true
      root.addArrangedSubview(stack([NSTextField(labelWithString: title), stack([field, button])], vertical: true))
    }
    root.addArrangedSubview(available); root.addArrangedSubview(detailScroll)
    let cancel = NSButton(title: "Cancel", target: nil, action: nil), review = NSButton(title: "Review Setup", target: nil, action: nil)
    review.keyEquivalent = "\r"
    var reviewing = false
    let actions = PreparationCompletion { [weak self] accepted in
      guard let self else { return }
      if !accepted {
        self.firstRunChoice = UUID(); self.firstRunClient?.cancelQueries()
        window.endSheet(sheet); self.sheetController = nil; self.preparationCompletion = nil; self.firstRunTargets = []
        return
      }
      guard !reviewing, ProjectCommand.validPath(data.stringValue), ProjectCommand.validPath(iso.stringValue) else { details.string = "Choose a data directory and an Ubuntu installer before reviewing."; return }
      reviewing = true; review.isEnabled = false
      let input = NativeFirstRunInput(setupID: setupID, dataLocation: data.stringValue, packageRoot: self.firstRunPackageRoot, isoPath: iso.stringValue)
      self.inspectFirstRun(input, choice: choice) { [weak self] result in
        guard let self else { return }
        reviewing = false; review.isEnabled = true
        switch result {
        case .failure(let error): details.string = error.localizedDescription
        case .success(let plan):
          details.string = plan.explanation
          guard plan.canCreate else { return }
          window.endSheet(sheet); self.sheetController = nil; self.preparationCompletion = nil; self.firstRunTargets = []
          self.reviewFirstRun(plan)
        }
      }
    }
    cancel.target = actions; cancel.action = #selector(PreparationCompletion.cancel(_:))
    review.target = actions; review.action = #selector(PreparationCompletion.prepare(_:))
    preparationCompletion = actions; firstRunTargets = [dataPicker, isoPicker, dataChoice]
    root.addArrangedSubview(stack([cancel, review])); sheet.contentView!.addSubview(root)
    NSLayoutConstraint.activate([root.leadingAnchor.constraint(equalTo: sheet.contentView!.leadingAnchor, constant: 20), root.trailingAnchor.constraint(equalTo: sheet.contentView!.trailingAnchor, constant: -20), root.topAnchor.constraint(equalTo: sheet.contentView!.topAnchor, constant: 20)])
    sheetController = NSWindowController(window: sheet); window.beginSheet(sheet)
    inspectFirstRun(NativeFirstRunInput(setupID: setupID, dataLocation: "", packageRoot: firstRunPackageRoot, isoPath: ""), choice: choice) { result in
      switch result {
      case .failure(let error): details.string = error.localizedDescription
      case .success(let plan):
        details.string = plan.explanation
        dataLocations = plan.alternatives; available.addItems(withTitles: dataLocations); available.isEnabled = !dataLocations.isEmpty
        if dataLocations.count == 1 && data.stringValue.isEmpty { data.stringValue = dataLocations[0] }
      }
    }
  }
  @objc func prepareProjectAssets(_ sender: Any?) {
    guard !closing, !switchingConfiguration, !hasActiveOperation, let client, let window, window.attachedSheet == nil else { return }
    guard FileManager.default.fileExists(atPath: firstRunPackageRoot + "/support/resources") else { prepareAdvancedProjectAssets(sender); return }
    let panel = NSOpenPanel(); panel.title = "Choose verified Ubuntu ARM64 desktop installer"
    panel.canChooseDirectories = false; panel.canChooseFiles = true; panel.allowsMultipleSelection = false
    if let saved = defaults.string(forKey: "FirstRunInstaller"), ProjectCommand.validPath(saved) { panel.directoryURL = URL(fileURLWithPath: saved).deletingLastPathComponent() }
    panel.beginSheetModal(for: window) { [weak self] response in
      guard let self, response == .OK, let iso = panel.url?.path else { return }
      let summary = NSAlert(); summary.messageText = "Prepare packaged project assets?"
      summary.informativeText = "Configuration: \(client.config)\nUbuntu installer: \(iso)\n\nBoxwarden checks packaged resources and existing preparation tools automatically. Retained partial preparation is inspected; uncertain artifacts are never silently replaced."
      summary.addButton(withTitle: "Prepare Assets"); summary.addButton(withTitle: "Advanced Existing Setup…"); summary.addButton(withTitle: "Cancel")
      summary.beginSheetModal(for: window) { [weak self] choice in
        guard let self else { return }
        if choice == .alertFirstButtonReturn { self.startPackagedPreparation(client: client, iso: iso) }
        else if choice == .alertSecondButtonReturn { self.prepareAdvancedProjectAssets(nil) }
      }
    }
  }
  func startPackagedPreparation(client: ProjectClient, iso: String) {
    guard !closing, !hasActiveOperation, let ticket = presentation.beginOperation() else { return }
    client.cancelQueries(); clipboardClient?.cancelQueries()
    do {
      activity = try client.start(.setupPreparePackaged(package: firstRunPackageRoot, iso: iso), progress: { [weak self] event in
        DispatchQueue.main.async { if let message = event.message { self?.report(message) } }
      }, completion: { [weak self] outcome in
        DispatchQueue.main.async {
          guard let self else { return }
          self.presentation.finishOperation(ticket); self.report(outcome.message)
          if case .success(.setup(let inspection)) = outcome {
            self.setupInspection = inspection
            if inspection.status == "ready" && self.defaults.string(forKey: "FirstRunCreateProjectConfiguration") == client.config { self.offerFirstProject = true }
          }
          try? self.recoverActivity(); self.refreshProjects(nil)
        }
      })
      report("Preparing packaged project assets…")
    } catch { presentation.finishOperation(ticket); report(error.localizedDescription) }
  }

  func inspectFirstRun(_ input: NativeFirstRunInput, choice: UUID, completion: @escaping (Result<NativeFirstRunPlan, Error>) -> Void) {
    guard let candidate = firstRunClient else { return }
    let token = candidate.queryToken()
    DispatchQueue.global(qos: .userInitiated).async { [weak self] in
      let result = Result { () throws -> NativeFirstRunPlan in
        guard case .firstRunPlan(let plan) = try candidate.query(.setupPlan(input), timeout: 120, queryToken: token) else { throw ProjectClientError.invalidResponse("Expected setup plan") }
        return plan
      }
      DispatchQueue.main.async { [weak self] in
        guard let self, !self.closing, self.firstRunChoice == choice else { return }
        completion(result)
      }
    }
  }
  func reviewFirstRun(_ plan: NativeFirstRunPlan) {
    guard !closing, !hasActiveOperation, plan.canCreate, let window else { return }
    let review = NSAlert(); review.messageText = "Create this Boxwarden setup?"
    review.informativeText = "Configuration: \(plan.configPath)\nPrivate state: \(plan.stateRoot)\nData location: \(plan.dataLocation)\nMounted volume: \(plan.mountPoint)\nUbuntu installer: \(plan.isoPath)\nAvailable: \(plan.availableBytes >> 30) GiB · reserved: \(plan.reserveBytes >> 30) GiB\n\(plan.existingEntries) existing entries will be preserved\n\nThis creates the reviewed configuration and prepares project assets. Existing files at the data location are retained. Host tools must already be initialized."
    review.informativeText += "\n\n" + plan.prerequisites.joined(separator: "\n")
    review.addButton(withTitle: "Create Setup & Prepare"); review.addButton(withTitle: "Cancel")
    review.beginSheetModal(for: window) { [weak self] response in
      guard response == .alertFirstButtonReturn else { return }
      self?.startFirstRun(plan)
    }
  }
  func startFirstRun(_ plan: NativeFirstRunPlan) {
    guard !closing, !hasActiveOperation, plan.canCreate, let ticket = presentation.beginOperation() else { return }
    do {
      let candidate = try ProjectClient(executable: executable, config: plan.configPath, activityDirectory: activityDirectory)
      if let previous = firstRunClient { retireFirstRunQueries(previous) }
      firstRunClient = candidate
      // Persist only the reviewed candidate after confirmation. SelectedConfiguration
      // remains the last admitted selection until read-only inspection accepts it.
      defaults.set(plan.configPath, forKey: "PendingFirstRunConfiguration")
      defaults.set(plan.isoPath, forKey: "FirstRunInstaller")
      defaults.set(plan.configPath, forKey: "FirstRunCreateProjectConfiguration")
      adoptFirstRunClient(candidate)
      activity = try candidate.start(.setupCreate(plan.input, digest: plan.expectedDigest), progress: { [weak self] event in
        DispatchQueue.main.async { if let message = event.message { self?.report(message) } }
      }, completion: { [weak self] outcome in
        DispatchQueue.main.async { [weak self] in
          guard let self else { return }
          self.presentation.finishOperation(ticket)
          self.report(outcome.message)
          try? self.recoverActivity()
          if case .success(.setup(let inspection)) = outcome, inspection.status == "ready" { self.offerFirstProject = true }
          // Inspect even a failed/unknown candidate. Never rerun setup.create automatically.
          self.useConfiguration(plan.configPath)
        }
      })
      report("Creating the reviewed configuration and preparing project assets…")
    } catch { presentation.finishOperation(ticket); report(error.localizedDescription) }
  }
}

final class FirstRunPicker: NSObject {
  let field: NSTextField; weak var parent: NSWindow?; let directory: Bool
  init(field: NSTextField, parent: NSWindow, directory: Bool) { self.field = field; self.parent = parent; self.directory = directory }
  @objc func choose(_ sender: Any?) {
    guard let parent else { return }
    let panel = NSOpenPanel(); panel.canChooseDirectories = directory; panel.canChooseFiles = !directory; panel.allowsMultipleSelection = false
    panel.beginSheetModal(for: parent) { [weak self] response in if response == .OK, let path = panel.url?.path { self?.field.stringValue = path } }
  }
}
final class FirstRunSelection: NSObject {
  let action: (Int) -> Void
  init(_ action: @escaping (Int) -> Void) { self.action = action }
  @objc func choose(_ sender: NSPopUpButton) { action(sender.indexOfSelectedItem) }
}
