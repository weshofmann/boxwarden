import AppKit
import UniformTypeIdentifiers

final class ProjectWindowController: NSWindowController, NSTableViewDataSource, NSTableViewDelegate, NSMenuItemValidation {
  let executable: String
  let privatePasteboard: String?
  let presentation = ProjectPresentation()
  let pendingClipboard = ClipboardPending()
  var sheetController: NSWindowController?
  var preferredSelection: String?
  var client: ProjectClient?
  var clipboardClient: CLIClient?
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
  let table = NSTableView()
  let configLabel = NSTextField(labelWithString: "Choose an initialized Boxwarden configuration to begin.")
  let detail = NSTextField(wrappingLabelWithString: "Projects will appear here after a configuration is selected.")
  let setupLabel = NSTextField(wrappingLabelWithString: "")
  let statusLabel = NSTextField(wrappingLabelWithString: "No configuration selected.")
  let clipboardLabel = NSTextField(wrappingLabelWithString: "Clipboard target: none")
  let progressText = NSTextView()
  let spinner = NSProgressIndicator()
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
  var pushButton: NSButton!
  var pullButton: NSButton!
  var cancelButton: NSButton!
  var revealButton: NSButton!
  var inspectUnknownButton: NSButton!
  var allowUnknownRetry = false
  var unknownClipboard = false
  var selectedProject: ProjectRecord? { projects.first { $0.name == presentation.selectedName } }
  var canReplaceSelectedProject: Bool { selectedProject?.observedState == "stopped" && selectedProject?.replacementPending == false && selectedProject?.availableActions.contains("inspect_session") == false }
  var hasActiveOperation: Bool { presentation.busy || recoveredBusy }

  init(executable: String, privatePasteboard: String? = nil) {
    self.executable = executable; self.privatePasteboard = privatePasteboard
    let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 1020, height: 790), styleMask: [.titled, .closable, .miniaturizable, .resizable], backing: .buffered, defer: false)
    window.title = "Boxwarden Projects"
    window.minSize = NSSize(width: 940, height: 740)
    window.center()
    super.init(window: window)
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
    chooseButton = button("Choose Configuration…", #selector(chooseConfiguration(_:)))
    createButton = button("New Project…", #selector(createProject(_:)))
    refreshButton = button("Refresh", #selector(refreshProjects(_:)))
    let header = stack([title, NSView(), createButton, refreshButton, chooseButton])
    configLabel.lineBreakMode = .byTruncatingMiddle; configLabel.isSelectable = true
    configLabel.textColor = .secondaryLabelColor
    setupLabel.textColor = .secondaryLabelColor
    let name = NSTableColumn(identifier: NSUserInterfaceItemIdentifier("name")); name.title = "Project"; name.width = 160
    let state = NSTableColumn(identifier: NSUserInterfaceItemIdentifier("state")); state.title = "State"; state.width = 120
    table.addTableColumn(name); table.addTableColumn(state)
    table.dataSource = self; table.delegate = self; table.rowHeight = 30
    table.allowsMultipleSelection = false
    table.setAccessibilityIdentifier("Projects")
    let listScroll = NSScrollView(); listScroll.documentView = table; listScroll.hasVerticalScroller = true
    listScroll.borderType = .bezelBorder
    detail.isSelectable = true; detail.font = .systemFont(ofSize: 13)
    openButton = button("Open Desktop", #selector(openProject(_:)))
    stopButton = button("Stop", #selector(stopProject(_:)))
    importButton = button("Import Project…", #selector(importProject(_:)))
    exportButton = button("Export Project…", #selector(exportProject(_:)))
    transactionsButton = button("Export Transactions…", #selector(showTransactions(_:)))
    replaceButton = button("Replace System…", #selector(replaceSystem(_:)))
    importRetryButton = button("Retry Import", #selector(retryImport(_:)))
    replaceRetryButton = button("Resume Replacement", #selector(retryReplacement(_:)))
    pushButton = button("HOST → GUEST", #selector(pushClipboard(_:)))
    pullButton = button("GUEST → HOST", #selector(pullClipboard(_:)))
    let clipNote = NSTextField(wrappingLabelWithString: "Text transfers only when you click a direction. No automatic sharing.")
    clipNote.font = .systemFont(ofSize: 11); clipNote.textColor = .secondaryLabelColor
    let right = stack([detail, stack([openButton, stopButton]), stack([importButton, exportButton]),
      stack([transactionsButton, importRetryButton]), stack([replaceButton, replaceRetryButton]),
      clipboardLabel, stack([pushButton, pullButton]), clipNote], vertical: true)
    right.spacing = 8
    let body = stack([listScroll, right]); body.alignment = .top
    listScroll.widthAnchor.constraint(equalToConstant: 285).isActive = true
    listScroll.heightAnchor.constraint(equalToConstant: 365).isActive = true
    right.widthAnchor.constraint(greaterThanOrEqualToConstant: 600).isActive = true
    detail.widthAnchor.constraint(equalTo: right.widthAnchor).isActive = true
    detail.heightAnchor.constraint(greaterThanOrEqualToConstant: 125).isActive = true
    spinner.style = .spinning; spinner.controlSize = .small; spinner.isDisplayedWhenStopped = false
    cancelButton = button("Interrupt Export", #selector(interruptExport(_:)))
    inspectUnknownButton = button("Review Unknown Outcome…", #selector(reviewUnknown(_:)))
    revealButton = button("Reveal Export in Finder", #selector(revealExport(_:)))
    let progressHeader = stack([spinner, statusLabel, NSView(), cancelButton])
    progressText.isEditable = false; progressText.isSelectable = true
    progressText.font = .monospacedSystemFont(ofSize: 11, weight: .regular)
    progressText.textContainerInset = NSSize(width: 6, height: 6)
    let log = NSScrollView(); log.documentView = progressText; log.hasVerticalScroller = true
    log.borderType = .bezelBorder; log.heightAnchor.constraint(greaterThanOrEqualToConstant: 105).isActive = true
    let limitations = NSTextField(wrappingLabelWithString: "Experimental private beta · alpha projects. Workspace files survive ordinary stop/start; processes do not. No live host shares. Stock networking permits guest access to gateway services; complete guest-to-host isolation is not claimed.")
    limitations.font = .systemFont(ofSize: 11); limitations.textColor = .secondaryLabelColor
    let root = stack([header, configLabel, setupLabel, body, progressHeader, log,
                      stack([revealButton, inspectUnknownButton]), limitations], vertical: true)
    root.spacing = 8; root.translatesAutoresizingMaskIntoConstraints = false
    content.addSubview(root)
    NSLayoutConstraint.activate([root.leadingAnchor.constraint(equalTo: content.leadingAnchor, constant: 20), root.trailingAnchor.constraint(equalTo: content.trailingAnchor, constant: -20), root.topAnchor.constraint(equalTo: content.topAnchor, constant: 18), root.bottomAnchor.constraint(equalTo: content.bottomAnchor, constant: -18)])
    for v in [header, configLabel, setupLabel, body, progressHeader, log, limitations] { v.widthAnchor.constraint(equalTo: root.widthAnchor).isActive = true }
  }
  func restoreConfiguration(override: String?) {
    if let path = override ?? UserDefaults.standard.string(forKey: "SelectedConfiguration"), ProjectCommand.validPath(path) { useConfiguration(path) }
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
    default: return true
    }
  }
  @objc func chooseConfiguration(_ sender: Any?) {
    guard !hasActiveOperation, let window, window.attachedSheet == nil else { return }
    let panel = NSOpenPanel(); panel.title = "Choose Initialized Boxwarden Configuration"
    panel.canChooseDirectories = false; panel.canChooseFiles = true; panel.allowsMultipleSelection = false
    panel.allowedContentTypes = [.json]
    panel.beginSheetModal(for: window) { [weak self] response in
      if response == .OK, let path = panel.url?.path { self?.useConfiguration(path) }
    }
  }
  func useConfiguration(_ path: String) {
    guard !hasActiveOperation else { return }
    do {
      let next = try ProjectClient(executable: executable, config: path)
      presentation.chooseConfiguration(path)
      client = next; clipboardClient = CLIClient(executable: executable, config: path, privateHostPasteboard: privatePasteboard)
      projects = []; clipboardTargets = []; setup = nil; snapshotAvailable = false
      lastExport = nil; activity = nil; recoveredActivities = []; preferredSelection = nil; allowUnknownRetry = false
      unknownClipboard = pendingClipboard.request(config: path) != nil
      if unknownClipboard { report("A previous explicit clipboard transfer has an unknown outcome. No payload was retained. Review the target before permitting another transfer.") }
      UserDefaults.standard.set(path, forKey: "SelectedConfiguration")
      configLabel.stringValue = "Configuration: " + path
      try recoverActivity()
      refreshProjects(nil)
    } catch { report(error.localizedDescription) }
  }
  @objc func refreshProjects(_ sender: Any?) {
    guard let client, let ticket = presentation.beginRefresh() else { return }
    render()
    let clipboardClient = self.clipboardClient
    DispatchQueue.global(qos: .userInitiated).async { [weak self] in
      do {
        let response = try client.query(.list)
        guard case .list(let list) = response else { throw ProjectClientError.invalidResponse("Expected project inventory") }
        let targets = (try? clipboardClient?.discover(domain: "alpha")) ?? []
        DispatchQueue.main.async { [weak self] in
          guard let self, self.presentation.finishRefresh(ticket, names: list.projects.map(\.name)) else { return }
          self.projects = list.projects; self.setup = list.setup; self.clipboardTargets = targets
          self.snapshotAvailable = true
          if let preferred = self.preferredSelection { self.presentation.select(preferred); self.preferredSelection = nil }
          self.table.reloadData(); self.selectVisibleRow(); self.render()
        }
      } catch {
        DispatchQueue.main.async { [weak self] in
          guard let self, self.presentation.finishRefresh(ticket, names: []) else { return }
          self.projects = []; self.clipboardTargets = []; self.snapshotAvailable = false; self.setup = nil
          self.table.reloadData(); self.report("Configuration or storage unavailable. Mount the configured storage and verify this configuration was initialized. " + error.localizedDescription)
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
    let idle = !hasActiveOperation
    let readyToAct = idle && !presentation.refreshing && snapshotAvailable
    chooseButton.isEnabled = idle
    createButton.isEnabled = readyToAct && setup?.status == "ready"
    refreshButton.isEnabled = client != nil && !presentation.refreshing
    table.isEnabled = idle
    let actions = Set(selectedProject?.availableActions ?? [])
    openButton.isEnabled = readyToAct && actions.contains("open")
    stopButton.isEnabled = readyToAct && actions.contains("stop")
    importButton.isEnabled = readyToAct && actions.contains("import")
    exportButton.isEnabled = readyToAct && actions.contains("export")
    transactionsButton.isEnabled = client != nil && selectedProject != nil && !presentation.refreshing
    replaceButton.isEnabled = readyToAct && canReplaceSelectedProject
    importRetryButton.isEnabled = readyToAct && actions.contains("import_retry")
    replaceRetryButton.isEnabled = readyToAct && actions.contains("rebuild_retry")
    pushButton.isEnabled = readyToAct && selectedClipboardTarget != nil && !unknownClipboard
    pullButton.isEnabled = pushButton.isEnabled
    cancelButton.isEnabled = presentation.busy && activity.map { ["project.export", "project.export.retry"].contains($0.operation) } == true
    revealButton.isEnabled = lastExport != nil
    inspectUnknownButton.isEnabled = idle && ((activity?.status == .unknown && activity?.acknowledgedAt == nil) || unknownClipboard)
    if hasActiveOperation || presentation.refreshing { spinner.startAnimation(nil) } else { spinner.stopAnimation(nil) }
    setupLabel.stringValue = setup.map { "Setup: \($0.status). \($0.guidance)" } ?? (client == nil ? "Select an existing initialized configuration. This app does not install host tools." : "Waiting for validated configuration and setup information.")
    if let p = selectedProject {
      let software = p.software.actions.map { "\($0.actionId): \($0.state)" }.joined(separator: ", ")
      detail.stringValue = "\(p.name)\nObserved: \(p.observedState ?? p.state) · Management ready: \(p.managementReady ? "yes" : "no")\nSoftware: \(p.software.status)\(software.isEmpty ? "" : " — " + software)\nWorkspace: \(p.workspace.sizeBytes >> 20) MiB · \(p.workspace.id)\nImport: \(p.importState.status)\n\(p.importState.guestPath.isEmpty ? p.workspace.mountPath : p.importState.guestPath)\n\(p.diagnostic)"
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
    report("Interruption requested. Waiting for the CLI outcome; the destination may be uncertain. Keep the project stopped and inspect Export Transactions before recovery.")
  }
  @objc func reviewUnknown(_ sender: Any?) {
    guard (activity?.status == .unknown && activity?.acknowledgedAt == nil) || unknownClipboard else { return }
    var message = "Refresh projects and inspect the destination or Export Transactions. Acknowledgement preserves the unknown outcome and allows later explicit commands; the backend still checks locks, retained intent and exact resources. Nothing is replayed automatically."
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
    guard !hasActiveOperation, let client, let ticket = presentation.beginOperation() else { return }
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
    let relevant = activities.filter { !["project.list", "project.status", "project.import.preview", "project.export.list"].contains($0.operation) }
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
    guard client != nil, !presentation.busy else { return }
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
    guard !hasActiveOperation, !presentation.refreshing, !unknownClipboard, let target = selectedClipboardTarget,
          let clipboardClient, let ticket = presentation.beginOperation() else { return }
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
