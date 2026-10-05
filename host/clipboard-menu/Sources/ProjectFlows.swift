import AppKit

extension ProjectWindowController {
  @objc func createProject(_ sender: Any?) {
    guard readyToAct, createButton.isEnabled, setup?.status == "ready", let window, window.attachedSheet == nil else { return }
    let a = NSAlert(); a.messageText = projects.isEmpty ? "Create your first project" : "Create a project"
    a.informativeText = "Preparation can take several minutes. The workspace is independent of the disposable system disk. This window remains responsive while the CLI prepares and starts the desktop."
    let name = NSTextField(string: ""); name.placeholderString = "Project name, e.g. myproject"; name.setAccessibilityIdentifier("New project name")
    let recipe = NSPopUpButton(); recipe.addItems(withTitles: ["Ubuntu desktop", "Desktop + action examples", "Desktop + ChatGPT client"]); recipe.setAccessibilityIdentifier("Recipe")
    let size = NSPopUpButton(); size.addItems(withTitles: ["64", "128", "256", "512", "1024", "2048", "4096"]); size.selectItem(withTitle: "512"); size.setAccessibilityIdentifier("Workspace MiB")
    let form = stack([NSTextField(labelWithString: "Name"), name, NSTextField(labelWithString: "Recipe"), recipe, NSTextField(labelWithString: "Workspace size (MiB)"), size], vertical: true)
    form.frame = NSRect(x: 0, y: 0, width: 380, height: 180); name.widthAnchor.constraint(equalToConstant: 380).isActive = true
    a.accessoryView = form; a.addButton(withTitle: "Create Project"); a.addButton(withTitle: "Cancel")
    a.beginSheetModal(for: window) { [weak self] response in
      guard response == .alertFirstButtonReturn, let self else { return }
      guard ProjectCommand.validToken(name.stringValue), let recipeName = ["desktop", "actions", "chatgpt"].dropFirst(recipe.indexOfSelectedItem).first, let bytes = Int(size.titleOfSelectedItem ?? "") else { self.report("Use a name containing letters, digits, dots, underscores or hyphens; no spaces or leading hyphen."); return }
      self.run(.create(name: name.stringValue, recipe: recipeName, sizeMiB: bytes))
    }
    window.attachedSheet?.makeFirstResponder(name)
  }
  @objc func importProject(_ sender: Any?) {
    guard readyToAct, selectedProject?.availableActions.contains("import") == true,
          let p = selectedProject, let client, let window, window.attachedSheet == nil else { return }
    let sheet = ImportProjectController(project: p.name, client: client) { [weak self] confirmed in
      guard let self else { return }
      self.run(.importProject(name: p.name, source: confirmed.source, exclusions: confirmed.exclusions, previewDigest: confirmed.digest))
    }
    sheetController = sheet
    window.beginSheet(sheet.window!) { [weak self] _ in self?.sheetController = nil }
  }
  @objc func exportProject(_ sender: Any?) {
    guard readyToAct, let p = selectedProject, p.availableActions.contains("export"), let window, window.attachedSheet == nil else { return }
    let panel = NSSavePanel(); panel.title = "Export Stopped Project"; panel.prompt = "Export"
    panel.nameFieldLabel = "New export folder:"
    panel.nameFieldStringValue = p.name + "-returned-" + String(Int(Date().timeIntervalSince1970))
    panel.message = "Choose a NEW folder name. Boxwarden creates it privately and returns validated guest files as data. Existing folders are refused; returned files are never run."
    panel.canCreateDirectories = false; panel.isExtensionHidden = false
    panel.beginSheetModal(for: window) { [weak self] response in
      guard response == .OK, let self, let path = panel.url?.path else { return }
      guard !FileManager.default.fileExists(atPath: path) else { self.report("Export needs a new folder. Choose a name that does not exist yet."); return }
      self.run(.export(name: p.name, destination: path))
    }
  }
  @objc func showTransactions(_ sender: Any?) {
    guard let p = selectedProject, let client, let window, window.attachedSheet == nil else { return }
    let sheet = ExportTransactionsController(project: p.name, client: client, canRetry: !hasActiveOperation) { [weak self] transaction in
      guard let self else { return }
      self.allowUnknownRetry = true
      self.run(.exportRetry(name: p.name, transaction: transaction))
    }
    sheetController = sheet
    window.beginSheet(sheet.window!) { [weak self] _ in self?.sheetController = nil }
    sheet.load()
  }
  @objc func replaceSystem(_ sender: Any?) {
    guard readyToAct, canReplaceSelectedProject, let p = selectedProject, let window, window.attachedSheet == nil else { return }
    let a = NSAlert(); a.alertStyle = .warning; a.messageText = "Replace the system for \(p.name)?"
    a.informativeText = "This discards system-local files, installed applications and running processes. The independent workspace and its files are retained. The project is stopped. Boxwarden prepares the selected recipe and starts its replacement. Export important work first."
    let recipe = NSPopUpButton(frame: NSRect(x: 0, y: 0, width: 240, height: 28)); recipe.addItems(withTitles: ["desktop", "actions", "chatgpt"]); recipe.setAccessibilityIdentifier("Replacement recipe")
    a.accessoryView = stack([NSTextField(labelWithString: "Recipe"), recipe], vertical: true); a.addButton(withTitle: "Cancel"); a.addButton(withTitle: "Replace System")
    a.beginSheetModal(for: window) { [weak self] response in
      guard response == .alertSecondButtonReturn, let recipe = recipe.titleOfSelectedItem else { return }
      self?.run(.rebuild(name: p.name, recipe: recipe))
    }
  }
}

// Sheets retain only UI selection and typed CLI results. The Go commands own
// transfer admission, filesystem checks, operation locks and recovery policy.
final class ImportProjectController: NSWindowController, NSTextFieldDelegate {
  let client: ProjectClient
  let confirm: (ConfirmedImport) -> Void
  let selection = ImportConfirmation()
  let source = NSTextField(string: "")
  let exclusions = NSTextField(string: "")
  let previewText = NSTextView()
  let note = NSTextField(wrappingLabelWithString: "Choose a private source directory you own. Source files are read only. Exclusions are explicit relative paths; nothing is excluded automatically. Do not change source permissions to satisfy this dialog.")
  let previewButton = NSButton(title: "Preview Selection", target: nil, action: nil)
  let importButton = NSButton(title: "Confirm Import", target: nil, action: nil)
  var pending = false
  init(project: String, client: ProjectClient, confirm: @escaping (ConfirmedImport) -> Void) {
    self.client = client; self.confirm = confirm
    let w = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 750, height: 530), styleMask: [.titled], backing: .buffered, defer: false); w.title = "Import into " + project
    super.init(window: w)
    source.placeholderString = "Source folder"; source.delegate = self; source.setAccessibilityIdentifier("Import source")
    exclusions.placeholderString = "Optional relative paths, separated by commas (e.g. .git, node_modules)"; exclusions.delegate = self; exclusions.setAccessibilityIdentifier("Import exclusions")
    let choose = NSButton(title: "Choose Source…", target: self, action: #selector(chooseSource))
    previewButton.target = self; previewButton.action = #selector(preview); importButton.target = self; importButton.action = #selector(commit); importButton.isEnabled = false
    let cancel = NSButton(title: "Cancel", target: self, action: #selector(closeSheet)); cancel.keyEquivalent = "\u{1b}"
    previewText.isEditable = false; previewText.font = .monospacedSystemFont(ofSize: 11, weight: .regular)
    let scroll = NSScrollView(); scroll.documentView = previewText; scroll.hasVerticalScroller = true; scroll.borderType = .bezelBorder
    let sourceRow = NSStackView(views: [source, choose]); sourceRow.spacing = 8
    let buttons = NSStackView(views: [previewButton, NSView(), cancel, importButton]); buttons.spacing = 8
    let root = NSStackView(views: [note, sourceRow, NSTextField(labelWithString: "Explicit exclusions"), exclusions, scroll, buttons]); root.orientation = .vertical; root.alignment = .leading; root.spacing = 10
    root.translatesAutoresizingMaskIntoConstraints = false; w.contentView!.addSubview(root)
    NSLayoutConstraint.activate([root.leadingAnchor.constraint(equalTo: w.contentView!.leadingAnchor, constant: 20), root.trailingAnchor.constraint(equalTo: w.contentView!.trailingAnchor, constant: -20), root.topAnchor.constraint(equalTo: w.contentView!.topAnchor, constant: 20), root.bottomAnchor.constraint(equalTo: w.contentView!.bottomAnchor, constant: -20), scroll.heightAnchor.constraint(greaterThanOrEqualToConstant: 250)])
    for view in [note, sourceRow, exclusions, scroll, buttons] { view.widthAnchor.constraint(equalTo: root.widthAnchor).isActive = true }
  }
  required init?(coder: NSCoder) { fatalError("Not supported") }
  func controlTextDidChange(_ notification: Notification) { changed() }
  func changed() { selection.source = source.stringValue; selection.exclusionsText = exclusions.stringValue; importButton.isEnabled = false; previewText.string = "Selection changed. Preview again before confirming." }
  @objc func chooseSource() {
    guard let window else { return }
    let panel = NSOpenPanel(); panel.canChooseFiles = false; panel.canChooseDirectories = true; panel.allowsMultipleSelection = false; panel.prompt = "Choose Source"
    panel.beginSheetModal(for: window) { [weak self] response in
      if response == .OK, let self, let path = panel.url?.path { self.source.stringValue = path; self.changed() }
    }
  }
  @objc func preview() {
    guard !pending else { return }; changed()
    let path = selection.source, excluded = selection.exclusions
    pending = true; previewButton.isEnabled = false; previewText.string = "Inspecting explicit selection…"
    let token = client.queryToken()
    DispatchQueue.global(qos: .userInitiated).async { [weak self, client] in
      let result = Result { try client.query(.importPreview(source: path, exclusions: excluded), queryToken: token) }
      DispatchQueue.main.async { [weak self] in
        guard let self else { return }; self.pending = false; self.previewButton.isEnabled = true
        do {
          guard case .preview(let p) = try result.get() else { throw ProjectClientError.invalidResponse("Expected import preview") }
          guard self.selection.acceptPreview(source: p.source, exclusions: p.exclusions, digest: p.digest) else { self.previewText.string = "Selection changed while previewing. Preview again."; return }
          let entries = p.entries.map { "\($0.kind)  \($0.path)\($0.size.map { "  (\($0) bytes)" } ?? "")" }.joined(separator: "\n")
          self.previewText.string = "\(p.fileCount) files · \(p.directoryCount) directories · \(p.totalBytes) bytes\nDigest: \(p.digest)\nExcluded: \(p.exclusions.isEmpty ? "none" : p.exclusions.joined(separator: ", "))\n\n" + entries
          self.importButton.isEnabled = true
        } catch { self.previewText.string = error.localizedDescription; self.importButton.isEnabled = false }
      }
    }
  }
  @objc func commit() { guard !pending, let confirmed = selection.confirmed else { return }; closeSheet(); confirm(confirmed) }
  @objc func closeSheet() { if let window { window.sheetParent?.endSheet(window) } }
}

final class ExportTransactionsController: NSWindowController {
  let project: String
  let client: ProjectClient
  let canRetry: Bool
  let retry: (String) -> Void
  var entries: [ProjectExportEntry] = []
  let choices = NSPopUpButton()
  let detail = NSTextField(wrappingLabelWithString: "Loading exports for this project…")
  let retryButton = NSButton(title: "Retry Selected Export…", target: nil, action: nil)
  let revealButton = NSButton(title: "Reveal Returned Files", target: nil, action: nil)
  init(project: String, client: ProjectClient, canRetry: Bool, retry: @escaping (String) -> Void) {
    self.project = project; self.client = client; self.canRetry = canRetry; self.retry = retry
    let w = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 740, height: 330), styleMask: [.titled], backing: .buffered, defer: false); w.title = "Exports & Recovery — " + project
    super.init(window: w)
    choices.target = self; choices.action = #selector(selected); choices.setAccessibilityIdentifier("Export transaction")
    retryButton.target = self; retryButton.action = #selector(retrySelected); retryButton.isEnabled = false
    revealButton.target = self; revealButton.action = #selector(reveal); revealButton.isEnabled = false
    detail.isSelectable = true
    let close = NSButton(title: "Close", target: self, action: #selector(closeSheet)); close.keyEquivalent = "\u{1b}"
    let note = NSTextField(wrappingLabelWithString: "Transactions are retained backend records. A published path is recorded metadata, not a fresh content verification. Retry uses the original destination and requires the project to stay stopped; it never overwrites a populated destination.")
    let buttons = NSStackView(views: [revealButton, retryButton, close]); buttons.spacing = 8
    let root = NSStackView(views: [note, choices, detail, buttons]); root.orientation = .vertical; root.alignment = .leading; root.spacing = 14
    root.translatesAutoresizingMaskIntoConstraints = false; w.contentView!.addSubview(root)
    NSLayoutConstraint.activate([root.leadingAnchor.constraint(equalTo: w.contentView!.leadingAnchor, constant: 20), root.trailingAnchor.constraint(equalTo: w.contentView!.trailingAnchor, constant: -20), root.topAnchor.constraint(equalTo: w.contentView!.topAnchor, constant: 20), root.bottomAnchor.constraint(lessThanOrEqualTo: w.contentView!.bottomAnchor, constant: -20), detail.heightAnchor.constraint(greaterThanOrEqualToConstant: 120)])
    for v in [note, choices, detail] { v.widthAnchor.constraint(equalTo: root.widthAnchor).isActive = true }
  }
  required init?(coder: NSCoder) { fatalError("Not supported") }
  var entry: ProjectExportEntry? { entries.indices.contains(choices.indexOfSelectedItem) ? entries[choices.indexOfSelectedItem] : nil }
  func load() {
    let token = client.queryToken()
    DispatchQueue.global(qos: .userInitiated).async { [weak self, client, project] in
      let result = Result { try client.query(.exportList(name: project), queryToken: token) }
      DispatchQueue.main.async { [weak self] in
        guard let self else { return }
        do {
          guard case .exports(let list) = try result.get() else { throw ProjectClientError.invalidResponse("Expected export transactions") }
          self.entries = list.exports; self.choices.addItems(withTitles: list.exports.map { "\($0.phase) — \($0.transaction)" }); self.selected()
        } catch { self.detail.stringValue = error.localizedDescription }
      }
    }
  }
  @objc func selected() {
    guard let e = entry else { detail.stringValue = "No retained export transactions for this project."; return }
    detail.stringValue = "Transaction: \(e.transaction)\nPhase: \(e.phase)\nDestination: \(e.destinationParent)\nReturned files: \(e.projectFiles.isEmpty ? "not published" : e.projectFiles)\nMatches current project: \(e.matchesCurrentBookmark ? "yes" : "no")"
    retryButton.isEnabled = canRetry && e.retryAvailable && e.matchesCurrentBookmark
    revealButton.isEnabled = !e.projectFiles.isEmpty
  }
  @objc func retrySelected() {
    guard let e = entry, canRetry, e.retryAvailable, e.matchesCurrentBookmark, let window else { return }
    let a = NSAlert(); a.messageText = "Retry this export?"; a.informativeText = "Retry transaction \(e.transaction) for \(project) into its recorded destination:\n\(e.destinationParent)\n\nReview the existing destination first. The backend refuses unsafe recovery. Keep the project stopped."
    a.addButton(withTitle: "Retry Export"); a.addButton(withTitle: "Cancel")
    a.beginSheetModal(for: window) { [weak self] response in guard response == .alertFirstButtonReturn, let self else { return }; self.closeSheet(); self.retry(e.transaction) }
  }
  @objc func reveal() { if let e = entry, !e.projectFiles.isEmpty { NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: e.projectFiles)]) } }
  @objc func closeSheet() { if let window { window.sheetParent?.endSheet(window) } }
}
