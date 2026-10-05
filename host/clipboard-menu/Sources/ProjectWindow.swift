import AppKit
import UniformTypeIdentifiers

final class ProjectWindowController: NSWindowController, NSTableViewDataSource, NSTableViewDelegate, NSMenuItemValidation {
  let executable: String
  let privatePasteboard: String?
  let presentation = ProjectPresentation()
  let pendingClipboard: ClipboardPending
  let activityDirectory: URL?
  let defaults: UserDefaults
  var sheetController: NSWindowController?
  var preferredSelection: String?
  var client: ProjectClient?
  var clipboardClient: CLIClient?
  private let queryRetirement = DispatchGroup()
  private(set) var switchingConfiguration = false
  private var desiredConfiguration = ""
  private var configurationChoice = UUID()
  private var validationClient: ProjectClient?
  var setupInspection: SetupInspection?
  var preparationPickers: [PreparationPicker] = []
  var preparationCompletion: PreparationCompletion?
  var prepareButton: NSButton!
  var helpButton: NSButton!
  var setupActions: NSStackView!
  var diagnosticsButton: NSButton!
  var diagnosticsViews: [NSView] = []
  var diagnosticsVisible = false
  private(set) var closing = false
  var projects: [ProjectRecord] = []
  var setup: ProjectSetup?
  var clipboardTargets: [MenuTarget] = []
  var activity: ProjectActivity?
  var recoveredActivities: [ProjectActivity] = []
  var recoveredBusy = false
  var snapshotAvailable = false
  var lastExport: String?
  var refreshTimer: Timer?
  var activityTimer: Timer?
  var progressLines: [String] = []
  var lastRefreshFailure: String?
  let limitations = NSTextField(wrappingLabelWithString: "")
  let table = NSTableView()
  let configLabel = NSTextField(labelWithString: "Set up Boxwarden or use an existing configuration to begin.")
  let detail = NSTextField(wrappingLabelWithString: "Projects will appear here after a configuration is selected.")
  let setupLabel = NSTextField(wrappingLabelWithString: "")
  let statusLabel = NSTextField(wrappingLabelWithString: "No configuration selected.")
  let clipboardLabel = NSTextField(wrappingLabelWithString: "Clipboard target: none")
  let progressText = NSTextView()
  let spinner = NSProgressIndicator()
  var firstRunTargets: [NSObject] = []
  var firstRunButton: NSButton!
  var firstRunClient: ProjectClient?
  var firstRunChoice = UUID()
  var offerFirstProject = false
  var chooseButton: NSButton!
  var createButton: NSButton!
  var refreshButton: NSButton!
  var openButton: NSButton!
  var stopButton: NSButton!
  var importButton: NSButton!
  var exportButton: NSButton!
  var transactionsButton: NSButton!
  var replaceButton: NSButton!
  var importRetryButton: NSButton!
  var replaceRetryButton: NSButton!
  var recoveryActions: NSStackView!
  var pushButton: NSButton!
  var pullButton: NSButton!
  var cancelButton: NSButton!
  var revealButton: NSButton!
  var inspectUnknownButton: NSButton!
  var allowUnknownRetry = false
  var unknownClipboard = false
  var selectedProject: ProjectRecord? { projects.first { $0.name == presentation.selectedName } }
  var canReplaceSelectedProject: Bool { setupInspection?.recipePreparationAvailable != false && selectedProject?.observedState == "stopped" && selectedProject?.replacementPending == false && selectedProject?.availableActions.contains("inspect_session") == false }
  var hasActiveOperation: Bool { presentation.busy || recoveredBusy }
  var readyToAct: Bool { !closing && !switchingConfiguration && !hasActiveOperation && snapshotAvailable }

  init(executable: String, privatePasteboard: String? = nil, activityDirectory: URL? = nil, defaults: UserDefaults = .standard, networkPolicy: String? = Bundle.main.object(forInfoDictionaryKey: "BoxwardenNetworkPolicy") as? String) {
    self.executable = executable; self.privatePasteboard = privatePasteboard
    self.activityDirectory = activityDirectory; self.defaults = defaults
    self.pendingClipboard = ClipboardPending(defaults: defaults)
    let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 960, height: 660), styleMask: [.titled, .closable, .miniaturizable, .resizable], backing: .buffered, defer: false)
    window.title = "Boxwarden Projects"
    window.minSize = NSSize(width: 760, height: 540)
    window.center()
    super.init(window: window)
    let notice: String
    switch networkPolicy {
    case "stock": notice = "Stock networking permits guest access to gateway services; complete guest-to-host isolation is not claimed."
    case "n1candidate": notice = "N1 candidate · not host-qualified. IPv4 host-service containment requires separate live validation; DHCP/DNS infrastructure remains reachable."
    default: notice = "Network policy unidentified. Inspect the bundled CLI with build-info --json before use."
    }
    limitations.stringValue = "Experimental private beta · alpha projects. Workspace files survive ordinary stop/start; processes do not. No live host shares. " + notice
    constructWindow()
    render()
  }
  required init?(coder: NSCoder) { fatalError("Not supported") }
  func button(_ title: String, _ action: Selector) -> NSButton {
    let b = NSButton(title: title, target: self, action: action)
    b.bezelStyle = .rounded
    b.setAccessibilityIdentifier(title)
    return b
  }
  func stack(_ views: [NSView], vertical: Bool = false) -> NSStackView {
    let s = NSStackView(views: views); s.orientation = vertical ? .vertical : .horizontal
    s.alignment = vertical ? .leading : .centerY; s.spacing = 8
    return s
  }
  func constructWindow() {
    guard let content = window?.contentView else { return }
    let title = NSTextField(labelWithString: "Boxwarden Projects"); title.font = .boldSystemFont(ofSize: 23)
    firstRunButton = button("Set up Boxwarden…", #selector(setUpBoxwarden(_:)))
    chooseButton = button("Use Existing Configuration…", #selector(chooseConfiguration(_:)))
    createButton = button("New Project…", #selector(createProject(_:)))
    refreshButton = button("Refresh", #selector(refreshProjects(_:)))
    prepareButton = button("Prepare Project Assets…", #selector(prepareProjectAssets(_:)))
    helpButton = button("Setup Help…", #selector(showSetupHelp(_:)))
    diagnosticsButton = button("Show Details", #selector(toggleDiagnostics(_:)))
    let header = stack([title, NSView(), createButton, refreshButton])
    let configurationRow = stack([configLabel, NSView(), firstRunButton, chooseButton])
    configLabel.lineBreakMode = .byTruncatingMiddle; configLabel.isSelectable = true
    configLabel.textColor = .secondaryLabelColor
    setupLabel.textColor = .labelColor
    setupActions = stack([prepareButton, helpButton])
    let name = NSTableColumn(identifier: NSUserInterfaceItemIdentifier("name")); name.title = "Project"; name.width = 155
    let state = NSTableColumn(identifier: NSUserInterfaceItemIdentifier("state")); state.title = "State"; state.width = 105
    table.addTableColumn(name); table.addTableColumn(state)
    table.dataSource = self; table.delegate = self; table.rowHeight = 30
    table.allowsMultipleSelection = false; table.setAccessibilityIdentifier("Projects")
    let listScroll = NSScrollView(); listScroll.documentView = table; listScroll.hasVerticalScroller = true; listScroll.borderType = .bezelBorder
    detail.isSelectable = true; detail.font = .systemFont(ofSize: 14)
    openButton = button("Open Desktop", #selector(openProject(_:)))
    stopButton = button("Stop", #selector(stopProject(_:)))
    importButton = button("Import Folder…", #selector(importProject(_:)))
    exportButton = button("Export Files…", #selector(exportProject(_:)))
    transactionsButton = button("Exports & Recovery…", #selector(showTransactions(_:)))
    replaceButton = button("Replace System…", #selector(replaceSystem(_:)))
    importRetryButton = button("Retry Import", #selector(retryImport(_:)))
    replaceRetryButton = button("Resume Replacement", #selector(retryReplacement(_:)))
    pushButton = button("Host → Guest", #selector(pushClipboard(_:)))
    pullButton = button("Guest → Host", #selector(pullClipboard(_:)))
    let clipNote = NSTextField(wrappingLabelWithString: "Text transfers only when you choose a direction. No automatic sharing.")
    clipNote.font = .systemFont(ofSize: 11); clipNote.textColor = .secondaryLabelColor
    recoveryActions = stack([importRetryButton, replaceRetryButton])
    let right = stack([detail, stack([openButton, stopButton]), stack([importButton, exportButton]), transactionsButton,
      clipboardLabel, stack([pushButton, pullButton]), clipNote, recoveryActions], vertical: true)
    let body = stack([listScroll, right]); body.alignment = .top
    listScroll.widthAnchor.constraint(equalToConstant: 270).isActive = true
    listScroll.heightAnchor.constraint(equalTo: body.heightAnchor).isActive = true
    body.heightAnchor.constraint(greaterThanOrEqualToConstant: 235).isActive = true
    right.widthAnchor.constraint(greaterThanOrEqualToConstant: 420).isActive = true
    detail.leadingAnchor.constraint(equalTo: right.leadingAnchor).isActive = true
    detail.trailingAnchor.constraint(equalTo: right.trailingAnchor, constant: -4).isActive = true
    spinner.style = .spinning; spinner.controlSize = .small; spinner.isDisplayedWhenStopped = false
    cancelButton = button("Interrupt Export", #selector(interruptExport(_:)))
    inspectUnknownButton = button("Review Unknown Outcome…", #selector(reviewUnknown(_:)))
    revealButton = button("Show Returned Files", #selector(revealExport(_:)))
    let progressHeader = stack([spinner, statusLabel, NSView(), cancelButton])
    progressText.isEditable = false; progressText.isSelectable = true
    progressText.font = .monospacedSystemFont(ofSize: 11, weight: .regular); progressText.textContainerInset = NSSize(width: 6, height: 6)
    let log = NSScrollView(); log.documentView = progressText; log.hasVerticalScroller = true; log.borderType = .bezelBorder
    log.heightAnchor.constraint(equalToConstant: 100).isActive = true
    let advanced = stack([replaceButton])
    diagnosticsViews = [advanced, log]; diagnosticsViews.forEach { $0.isHidden = true }
    limitations.font = .systemFont(ofSize: 11); limitations.textColor = .secondaryLabelColor
    let root = stack([header, configurationRow, setupLabel, setupActions, body, progressHeader,
      stack([revealButton, inspectUnknownButton, diagnosticsButton]), advanced, log, limitations], vertical: true)
    root.spacing = 8; root.detachesHiddenViews = true; root.translatesAutoresizingMaskIntoConstraints = false
    content.addSubview(root)
    NSLayoutConstraint.activate([root.leadingAnchor.constraint(equalTo: content.leadingAnchor, constant: 20), root.trailingAnchor.constraint(equalTo: content.trailingAnchor, constant: -20), root.topAnchor.constraint(equalTo: content.topAnchor, constant: 18), root.bottomAnchor.constraint(equalTo: content.bottomAnchor, constant: -18)])
    for v in [header, configurationRow, setupLabel, body, progressHeader, log, limitations] { v.widthAnchor.constraint(equalTo: root.widthAnchor).isActive = true }
    window?.recalculateKeyViewLoop()
  }
  @objc func toggleDiagnostics(_ sender: Any?) {
    diagnosticsVisible.toggle(); diagnosticsViews.forEach { $0.isHidden = !diagnosticsVisible }
    diagnosticsButton.title = diagnosticsVisible ? "Hide Details" : "Show Details"
    render()
  }
  func restoreConfiguration(override: String?) {
    if let path = override ?? defaults.string(forKey: "PendingFirstRunConfiguration") ?? defaults.string(forKey: "SelectedConfiguration"), ProjectCommand.validPath(path) { useConfiguration(path) }
    refreshTimer = Timer.scheduledTimer(withTimeInterval: 15, repeats: true) { [weak self] _ in
      guard let self, !self.hasActiveOperation, self.window?.attachedSheet == nil else { return }
      self.refreshProjects(nil)
    }
    activityTimer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { [weak self] _ in self?.pollActivity() }
  }
  func validateMenuItem(_ menuItem: NSMenuItem) -> Bool {
    guard window?.attachedSheet == nil else { return false }
    switch menuItem.action {
    case #selector(createProject(_:)): return createButton.isEnabled
    case #selector(chooseConfiguration(_:)): return chooseButton.isEnabled
    case #selector(refreshProjects(_:)): return refreshButton.isEnabled
    case #selector(pushClipboard(_:)): return pushButton.isEnabled
    case #selector(pullClipboard(_:)): return pullButton.isEnabled
    default: return true
    }
  }
  @objc func chooseConfiguration(_ sender: Any?) {
    guard !closing, !hasActiveOperation, let window, window.attachedSheet == nil else { return }
    let panel = NSOpenPanel(); panel.title = "Choose Boxwarden Configuration"
    panel.canChooseDirectories = false; panel.canChooseFiles = true; panel.allowsMultipleSelection = false
    panel.allowedContentTypes = [.json]
    panel.beginSheetModal(for: window) { [weak self] response in
      if response == .OK, let path = panel.url?.path { self?.useConfiguration(path) }
    }
  }
  func useConfiguration(_ path: String) {
    guard !closing, !hasActiveOperation, ProjectCommand.validPath(path) else { return }
    desiredConfiguration = path
    configurationChoice = UUID()
    statusLabel.stringValue = "Checking selected configuration…"
    progressLines.removeAll(); progressText.string = ""
    if switchingConfiguration { validationClient?.cancelQueries(); return }
    switchingConfiguration = true
    presentation.cancelRefresh()
    let previous = client, previousClipboard = clipboardClient
    client = nil; clipboardClient = nil
    queryRetirement.enter()
    render()
    drainQueries(project: previous, clipboard: previousClipboard) { [self] in
      DispatchQueue.main.async { [self] in validateDesiredConfiguration() }
    }
  }
  private func validateDesiredConfiguration() {
    guard !closing else { switchingConfiguration = false; queryRetirement.leave(); return }
    let path = desiredConfiguration
    let choice = configurationChoice
    do {
      let candidate = try ProjectClient(executable: executable, config: path, activityDirectory: activityDirectory)
      validationClient = candidate
      let token = candidate.queryToken()
      DispatchQueue.global(qos: .userInitiated).async { [self] in
        let result = Result { () throws -> SetupInspection in
          guard case .setup(let inspection) = try candidate.query(.setupInspect, queryToken: token), inspection.configPath == path else { throw ProjectClientError.invalidResponse("Setup inspection did not match the selected configuration") }
          return inspection
        }
        DispatchQueue.main.async { [self] in
          // Query completion owns child retirement. A newer selection is inspected
          // only after the older read-only client has fully reaped its child.
          candidate.disposeQueries { [self] in
            DispatchQueue.main.async { [self] in
              validationClient = nil
              if !closing && choice != configurationChoice { validateDesiredConfiguration(); return }
              defer { switchingConfiguration = false; queryRetirement.leave(); render() }
              guard !closing else { return }
              do {
                let inspection = try result.get()
                if !inspection.selectionAcceptable {
                  if presentation.configPath.isEmpty { setupInspection = inspection }
                  try restoreActiveClients()
                  if presentation.configPath.isEmpty, defaults.string(forKey: "PendingFirstRunConfiguration") == path,
                     let previous = defaults.string(forKey: "SelectedConfiguration"), previous != path, ProjectCommand.validPath(previous) {
                    DispatchQueue.main.async { [weak self] in self?.useConfiguration(previous) }
                  }
                  report(inspection.title + ". " + inspection.guidance + (presentation.configPath.isEmpty ? "" : " The previous configuration is still selected."))
                  return
                }
                presentation.chooseConfiguration(path)
                projects = []; clipboardTargets = []; setup = nil; snapshotAvailable = false
                setupInspection = inspection
                lastExport = nil; activity = nil; recoveredActivities = []; preferredSelection = nil; allowUnknownRetry = false
                lastRefreshFailure = nil
                defaults.set(path, forKey: "SelectedConfiguration")
                if defaults.string(forKey: "PendingFirstRunConfiguration") == path { defaults.removeObject(forKey: "PendingFirstRunConfiguration") }
                configLabel.stringValue = "Configuration: " + path
                try restoreActiveClients()
                statusLabel.stringValue = inspection.status == "ready" ? "Refreshing selected configuration…" : inspection.guidance
                switchingConfiguration = false
                if inspection.status == "ready" {
                  offerFirstProject = defaults.string(forKey: "FirstRunCreateProjectConfiguration") == path
                  refreshProjects(nil)
                }
              } catch {
                try? restoreActiveClients()
                report("Unable to check this configuration. " + error.localizedDescription + (presentation.configPath.isEmpty ? "" : " The previous configuration is still selected."))
              }
            }
          }
        }
      }
    } catch { switchingConfiguration = false; queryRetirement.leave(); try? restoreActiveClients(); report(error.localizedDescription) }
  }
  private func restoreActiveClients() throws {
    guard !presentation.configPath.isEmpty else { return }
    let path = presentation.configPath
    client = try ProjectClient(executable: executable, config: path, activityDirectory: activityDirectory)
    clipboardClient = CLIClient(executable: executable, config: path, privateHostPasteboard: privatePasteboard)
    unknownClipboard = pendingClipboard.request(config: path) != nil
    try recoverActivity()
  }
  private func drainQueries(project: ProjectClient?, clipboard: CLIClient?, completion: @escaping () -> Void) {
    let drain = DispatchGroup()
    if let project { drain.enter(); project.disposeQueries { drain.leave() } }
    if let clipboard { drain.enter(); clipboard.disposeQueries { drain.leave() } }
    drain.notify(queue: .global(), execute: completion)
  }
  func shutdownQueries(_ completion: @escaping () -> Void) {
    closing = true
    refreshTimer?.invalidate(); activityTimer?.invalidate()
    presentation.cancelRefresh()
    snapshotAvailable = false
    validationClient?.cancelQueries()
    firstRunChoice = UUID()
    if let firstRunClient { queryRetirement.enter(); firstRunClient.disposeQueries { [self] in queryRetirement.leave() } }
    queryRetirement.enter()
    drainQueries(project: client, clipboard: clipboardClient) { [self] in queryRetirement.leave() }
    queryRetirement.notify(queue: .main, execute: completion)
    render()
  }
  func retireFirstRunQueries(_ previous: ProjectClient) {
    queryRetirement.enter()
    previous.disposeQueries { [self] in queryRetirement.leave() }
  }
  func adoptFirstRunClient(_ candidate: ProjectClient) {
    presentation.cancelRefresh()
    let previous = client, previousClipboard = clipboardClient
    client = candidate; clipboardClient = nil
    queryRetirement.enter()
    drainQueries(project: previous, clipboard: previousClipboard) { [self] in queryRetirement.leave() }
  }
  private func cancelObservation() { client?.cancelQueries(); clipboardClient?.cancelQueries() }
  @objc func refreshProjects(_ sender: Any?) {
    guard !closing, !switchingConfiguration, let client, let ticket = presentation.beginRefresh() else { return }
    render()
    let clipboardClient = self.clipboardClient
    let queryToken = client.queryToken(), clipboardToken = clipboardClient?.queryToken()
    let inspectSetup = setupInspection != nil
    DispatchQueue.global(qos: .userInitiated).async { [weak self] in
      do {
        let inspection: SetupInspection?
        if inspectSetup {
          guard case .setup(let current) = try client.query(.setupInspect, queryToken: queryToken) else { throw ProjectClientError.invalidResponse("Expected setup inspection") }
          inspection = current
          if current.status != "ready" {
            DispatchQueue.main.async { [weak self] in
              guard let self, !self.closing, self.presentation.finishRefresh(ticket, names: []) else { return }
              self.setupInspection = current; self.setup = nil; self.snapshotAvailable = false
              self.projects = []; self.clipboardTargets = []; self.table.reloadData()
              let message = current.title + ". " + current.guidance
              self.statusLabel.stringValue = message
              if self.lastRefreshFailure != message { self.lastRefreshFailure = message; self.appendProgress(message) }
              self.render()
            }
            return
          }
        } else { inspection = nil }
        let response = try client.query(.list, queryToken: queryToken)
        guard case .list(let list) = response else { throw ProjectClientError.invalidResponse("Expected project inventory") }
        let targets = (try? clipboardClient?.discover(domain: "alpha", queryToken: clipboardToken)) ?? []
        DispatchQueue.main.async { [weak self] in
          guard let self, !self.closing, self.presentation.finishRefresh(ticket, names: list.projects.map(\.name)) else { return }
          if let inspection { self.setupInspection = inspection }
          self.projects = list.projects; self.setup = list.setup; self.clipboardTargets = targets
          self.snapshotAvailable = true
          if let failure = self.lastRefreshFailure {
            self.lastRefreshFailure = nil
            let recovered = "Configuration and storage available. Project inventory refreshed."
            self.appendProgress(recovered)
            if self.statusLabel.stringValue == failure { self.statusLabel.stringValue = recovered }
          }
          if self.statusLabel.stringValue == "Refreshing selected configuration…" { self.statusLabel.stringValue = "Project inventory refreshed." }
          if let preferred = self.preferredSelection { self.presentation.select(preferred); self.preferredSelection = nil }
          self.table.reloadData(); self.selectVisibleRow(); self.render()
          if self.offerFirstProject && self.setupInspection?.status == "ready" && !self.hasActiveOperation && self.window?.attachedSheet == nil {
            self.offerFirstProject = false; self.defaults.removeObject(forKey: "FirstRunCreateProjectConfiguration")
            if list.projects.isEmpty { self.createProject(nil) }
          }
        }
      } catch {
        DispatchQueue.main.async { [weak self] in
          guard let self, !self.closing, self.presentation.finishRefresh(ticket, names: []) else { return }
          self.projects = []; self.clipboardTargets = []; self.snapshotAvailable = false; self.setup = nil
          self.table.reloadData()
          let message = "Project inventory could not be refreshed. Choose the configuration again to recheck setup and storage. " + error.localizedDescription
          self.statusLabel.stringValue = message
          if self.lastRefreshFailure != message { self.lastRefreshFailure = message; self.appendProgress(message) }
          self.render()
        }
      }
    }
  }
  func numberOfRows(in tableView: NSTableView) -> Int { projects.count }
  func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
    guard projects.indices.contains(row) else { return nil }
    let p = projects[row]
    return NSTextField(labelWithString: tableColumn?.identifier.rawValue == "name" ? p.name : p.state)
  }
  func tableViewSelectionDidChange(_ notification: Notification) {
    if projects.indices.contains(table.selectedRow) { presentation.select(projects[table.selectedRow].name) }
    if !presentation.busy { activity = chooseVisibleActivity() }
    selectVisibleRow(); render()
  }
  func selectVisibleRow() {
    if let name = presentation.selectedName, let row = projects.firstIndex(where: { $0.name == name }), table.selectedRow != row { table.selectRowIndexes(IndexSet(integer: row), byExtendingSelection: false) }
  }
  var selectedClipboardTarget: MenuTarget? {
    guard let p = selectedProject else { return nil }
    return clipboardTargets.first { $0.domain == "alpha" && $0.session == p.name && $0.sessionID == p.sessionId && $0.backendObject == p.backendObject && $0.available && $0.isValid }
  }
  func render() {
    let idle = !closing && !hasActiveOperation
    firstRunButton.isEnabled = idle && !switchingConfiguration
    firstRunButton.isHidden = !presentation.configPath.isEmpty
    chooseButton.isEnabled = idle
    createButton.isEnabled = readyToAct && setup?.status == "ready" && setupInspection?.recipePreparationAvailable != false
    prepareButton.isEnabled = idle && !switchingConfiguration && setupInspection?.selectionAcceptable == true && ["domain_uninitialized", "project_setup_missing"].contains(setupInspection?.status ?? "")
    prepareButton.isHidden = presentation.configPath.isEmpty || setupInspection?.status == "ready"
    helpButton.isHidden = setupInspection?.status == "ready" && setupInspection?.recipePreparationAvailable != false
    setupActions.isHidden = prepareButton.isHidden && helpButton.isHidden
    importRetryButton.isHidden = !(selectedProject?.availableActions.contains("import_retry") ?? false)
    replaceRetryButton.isHidden = !(selectedProject?.availableActions.contains("rebuild_retry") ?? false)
    recoveryActions.isHidden = importRetryButton.isHidden && replaceRetryButton.isHidden
    refreshButton.isEnabled = !closing && client != nil && !presentation.busy && !presentation.refreshing
    table.isEnabled = idle
    let actions = Set(selectedProject?.availableActions ?? [])
    openButton.isEnabled = readyToAct && actions.contains("open")
    stopButton.isEnabled = readyToAct && actions.contains("stop")
    importButton.isEnabled = readyToAct && actions.contains("import")
    exportButton.isEnabled = readyToAct && actions.contains("export")
    transactionsButton.isEnabled = !closing && client != nil && selectedProject != nil && snapshotAvailable
    replaceButton.isEnabled = readyToAct && canReplaceSelectedProject
    importRetryButton.isEnabled = readyToAct && actions.contains("import_retry")
    replaceRetryButton.isEnabled = readyToAct && actions.contains("rebuild_retry")
    pushButton.isEnabled = readyToAct && selectedClipboardTarget != nil && !unknownClipboard
    pullButton.isEnabled = pushButton.isEnabled
    cancelButton.isEnabled = presentation.busy && activity.map { ["project.export", "project.export.retry"].contains($0.operation) } == true
    revealButton.isEnabled = lastExport != nil
    inspectUnknownButton.isEnabled = idle && ((activity?.status == .unknown && activity?.acknowledgedAt == nil) || unknownClipboard)
    if switchingConfiguration || hasActiveOperation || presentation.refreshing { spinner.startAnimation(nil) } else { spinner.stopAnimation(nil) }
    if hasActiveOperation, let activity, activity.operation == "project.create" {
      setupLabel.stringValue = "Creating \(activity.projectName ?? "project"). First preparation may take tens of minutes; progress appears below."
    } else if let inspection = setupInspection {
      setupLabel.stringValue = inspection.status == "ready" ? (inspection.recipePreparationAvailable == false ? "Existing project setup is ready. Recipe creation requires an explicit setup update; see Setup Help." : "Project setup is ready. Create a project or select one below.") : inspection.title + ". " + inspection.guidance
    } else { setupLabel.stringValue = "Start setup: create a Boxwarden configuration and prepare project assets, or use an existing configuration." }
    if let p = selectedProject {
      let state = p.observedState ?? p.state
      let next: String
      if p.replacementPending { next = "System replacement is interrupted. Review Details or resume the recorded replacement." }
      else if state == "stopped" {
        next = p.availableActions.contains("import")
          ? "Open Desktop starts this project again. You can import or export files while it is stopped."
          : "Open Desktop starts this project again. You can export files while it is stopped."
      }
      else if p.managementReady { next = "Desktop is running. Stop the project before exporting files or replacing its system." }
      else { next = "The project is \(state). Management is not ready; refresh or inspect Details before continuing." }
      detail.stringValue = "\(p.name)\n\(next)\nWorkspace: \(p.workspace.sizeBytes >> 20) MiB · files survive stop/start\n" + (p.importState.guestPath.isEmpty ? p.workspace.mountPath : "Imported files: " + p.importState.guestPath)
      if diagnosticsVisible {
        let software = state == "stopped" && p.software.status == "unavailable" ? "Not checked while stopped" : p.software.status
        detail.stringValue += "\nSystem: \(p.backendObject)\nWorkspace ID: \(p.workspace.id)\nSoftware: \(software)\n\(p.diagnostic)"
      }
      clipboardLabel.stringValue = "Clipboard target: alpha / \(p.name)" + (selectedClipboardTarget == nil ? " — unavailable" : " — ready") + (privatePasteboard == nil ? "" : " · synthetic private pasteboard")
    } else {
      detail.stringValue = projects.isEmpty && snapshotAvailable ? "No projects in this configuration. Create a project after setup is ready." : "Select a project to inspect its workspace and current state."
      clipboardLabel.stringValue = "Clipboard target: none"
    }
  }
  func report(_ message: String) { statusLabel.stringValue = message; appendProgress(message); render() }
  func appendProgress(_ message: String) {
    progressLines.append(message)
    if progressLines.count > 120 { progressLines.removeFirst(progressLines.count - 120) }
    progressText.string = progressLines.joined(separator: "\n")
    progressText.scrollToEndOfDocument(nil)
  }
  func alert(_ title: String, _ message: String, action: String) -> Bool {
    let a = NSAlert(); a.messageText = title; a.informativeText = message
    a.addButton(withTitle: action); a.addButton(withTitle: "Cancel")
    return a.runModal() == .alertFirstButtonReturn
  }
  @objc func openProject(_ sender: Any?) { if let p = selectedProject { run(.open(name: p.name)) } }
  @objc func stopProject(_ sender: Any?) { if let p = selectedProject { run(.stop(name: p.name)) } }
  @objc func retryImport(_ sender: Any?) { if let p = selectedProject { run(.importRetry(name: p.name)) } }
  @objc func retryReplacement(_ sender: Any?) { if let p = selectedProject, alert("Resume system replacement?", "Continue the recorded replacement for \(p.name), preserving its workspace. The backend will recheck retained state.", action: "Resume Replacement") { run(.rebuildRetry(name: p.name)) } }
  @objc func revealExport(_ sender: Any?) { if let path = lastExport { NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: path)]) } }
  @objc func interruptExport(_ sender: Any?) {
    guard let activity, client?.cancelActiveActivity(activity.id) == true else { return }
    report("Interruption requested. Waiting for the CLI outcome; the destination may be uncertain. Keep the project stopped and choose Exports & Recovery before recovery.")
  }
  @objc func reviewUnknown(_ sender: Any?) {
    guard (activity?.status == .unknown && activity?.acknowledgedAt == nil) || unknownClipboard else { return }
    var message = "Refresh projects and inspect the destination or choose Exports & Recovery. Acknowledgement preserves the unknown outcome and allows later explicit commands; the backend still checks locks, retained intent and exact resources. Nothing is replayed automatically."
    if unknownClipboard, let pending = pendingClipboard.request(config: presentation.configPath) {
      message += "\nPrevious clipboard request: alpha / \(pending["project"] ?? "unknown") — \(pending["direction"] ?? "unknown direction"). Its destination outcome is unknown; no payload was retained."
    }
    guard alert("Prior request outcome is unknown", message, action: "Acknowledge Unknown Outcome") else { return }
    do {
      if let activity, activity.status == .unknown, activity.acknowledgedAt == nil { try client?.acknowledgeUnknownActivity(activity.id) }
      if unknownClipboard { pendingClipboard.acknowledge(config: presentation.configPath); unknownClipboard = false }
      try recoverActivity()
      report("Unknown outcome acknowledged. Choose the appropriate explicit action after reviewing current state.")
    } catch { report(error.localizedDescription) }
  }
  func run(_ command: ProjectCommand) {
    guard readyToAct, let client, let ticket = presentation.beginOperation() else { return }
    cancelObservation()
    activity = nil
    statusLabel.stringValue = command.operation + " in progress"
    appendProgress("Starting " + command.operation + " for " + (command.projectName ?? "selection"))
    render()
    do {
      activity = try client.start(command, allowRetryAfterUnknown: allowUnknownRetry, progress: { [weak self] event in
        DispatchQueue.main.async { [weak self] in
          guard let self, let message = event.message else { return }
          self.statusLabel.stringValue = message; self.appendProgress(message)
        }
      }, completion: { [weak self] outcome in
        DispatchQueue.main.async { [weak self] in
          guard let self else { return }
          self.presentation.finishOperation(ticket)
          self.statusLabel.stringValue = outcome.message
          self.appendProgress(outcome.message)
          if case .success(let response) = outcome {
            if case .project(let project) = response { self.preferredSelection = project.name }
            if case .export(let receipt) = response {
              self.acceptExport(receipt)
            }
          }
          try? self.recoverActivity()
          self.refreshProjects(nil); self.render()
        }
      })
      allowUnknownRetry = false
      render()
    } catch { presentation.finishOperation(ticket); report(error.localizedDescription) }
  }
  func chooseVisibleActivity() -> ProjectActivity? {
    if let live = recoveredActivities.first(where: { $0.status == .running }) { return live }
    let selected = recoveredActivities.filter { $0.projectName == presentation.selectedName }
    return selected.first(where: { $0.status == .unknown && $0.acknowledgedAt == nil }) ?? selected.first
  }
  func acceptExport(_ receipt: ProjectExportReceipt) {
    lastExport = receipt.projectFiles.isEmpty ? nil : receipt.projectFiles
    let summary = lastExport.map { "Returned files: " + $0 } ?? "Export transaction phase: " + receipt.phase + "; no files were published."
    statusLabel.stringValue = summary; appendProgress(summary)
  }
  func recoverActivity() throws {
    guard let client else { return }
    let activities = try client.recoverActivities()
    let relevant = activities.filter { !["setup.inspect", "setup.plan", "project.list", "project.status", "project.import.preview", "project.export.list"].contains($0.operation) }
    recoveredActivities = relevant
    recoveredBusy = relevant.contains { $0.status == .running }
    if !presentation.busy { activity = chooseVisibleActivity() }
    if let last = activity, last.status == .running || last.status == .unknown {
      statusLabel.stringValue = "Previous \(last.operation): \(last.message)"
    }
    if let receipt = activity {
      for event in try client.activityEvents(receipt.id) {
        if case .export(let result) = event.response { lastExport = result.projectFiles.isEmpty ? nil : result.projectFiles }
      }
    }
  }
  func pollActivity() {
    guard !closing, !switchingConfiguration, client != nil, !presentation.busy else { return }
    let wasBusy = recoveredBusy
    do {
      try recoverActivity()
      if recoveredBusy, let activity, let events = try client?.activityEvents(activity.id), let message = events.last?.message {
        statusLabel.stringValue = message
      }
      if wasBusy && !recoveredBusy { refreshProjects(nil) }
      render()
    } catch { recoveredBusy = false; report("Activity outcome unavailable: " + error.localizedDescription) }
  }
  @objc func pushClipboard(_ sender: Any?) { transferClipboard(.push) }
  @objc func pullClipboard(_ sender: Any?) { transferClipboard(.pull) }
  func transferClipboard(_ direction: TransferDirection) {
    guard readyToAct, !unknownClipboard, let target = selectedClipboardTarget,
          let clipboardClient, let ticket = presentation.beginOperation() else { return }
    cancelObservation()
    let request = TransferRequest(id: UUID(), target: target, direction: direction)
    let requestConfig = presentation.configPath
    guard pendingClipboard.begin(config: requestConfig, metadata: ["request": request.id.uuidString,
      "project": target.session, "sessionID": target.sessionID, "generation": target.generation,
      "direction": direction.rawValue]) else { presentation.finishOperation(ticket); unknownClipboard = true; report("Review the previous clipboard outcome before another transfer."); return }
    statusLabel.stringValue = "Explicit \(direction == .push ? "HOST → GUEST" : "GUEST → HOST") transfer to alpha / \(target.session)"
    render()
    let child = clipboardClient.transfer(request) { [weak self] result in
      DispatchQueue.main.async { [weak self] in
        guard let self else { return }
        self.presentation.finishOperation(ticket)
        self.pendingClipboard.complete(config: requestConfig, requestID: request.id.uuidString, succeeded: result == .success)
        self.unknownClipboard = self.pendingClipboard.request(config: requestConfig) != nil
        switch result {
        case .success: self.statusLabel.stringValue = "Clipboard transfer completed."
        case .failed, .cancelled: self.statusLabel.stringValue = "Clipboard transfer did not confirm success; destination outcome is unknown."
        }
        self.refreshProjects(nil); self.render()
      }
    }
    if child == nil { pendingClipboard.complete(config: requestConfig, requestID: request.id.uuidString, succeeded: true); presentation.finishOperation(ticket); report("Clipboard transfer could not start.") }
  }
}
