import Foundation

// Main-thread presentation state. The backend re-admits every effect; these
// tokens only prevent stale callbacks and repeated clicks in this window.
final class ProjectPresentation {
  private(set) var configPath = ""
  private(set) var selectedName: String?
  private(set) var refreshing = false
  private(set) var busy = false
  private var refreshID: UUID?
  private var operationID: UUID?
  private var projectNames: [String] = []

  func chooseConfiguration(_ path: String) {
    guard !busy else { return }
    configPath = path
    selectedName = nil
    projectNames = []
    refreshID = nil
    refreshing = false
  }
  func beginRefresh() -> UUID? {
    guard !refreshing, !configPath.isEmpty else { return nil }
    let id = UUID()
    refreshID = id
    refreshing = true
    return id
  }
  @discardableResult func finishRefresh(_ id: UUID, names: [String]) -> Bool {
    guard refreshID == id else { return false }
    refreshID = nil
    refreshing = false
    projectNames = names
    if selectedName == nil || !names.contains(selectedName!) { selectedName = names.first }
    return true
  }
  func select(_ name: String?) {
    guard !busy else { return }
    selectedName = name.flatMap { projectNames.contains($0) ? $0 : nil }
  }
  func beginOperation() -> UUID? {
    guard !busy else { return nil }
    let id = UUID()
    operationID = id
    busy = true
    return id
  }
  func finishOperation(_ id: UUID) {
    guard operationID == id else { return }
    operationID = nil
    busy = false
  }
}

struct ConfirmedImport {
  let source: String
  let exclusions: [String]
  let digest: String
}

final class ImportConfirmation {
  var source = "" { didSet { if source != oldValue { confirmed = nil } } }
  var exclusionsText = "" { didSet { if exclusionsText != oldValue { confirmed = nil } } }
  private(set) var confirmed: ConfirmedImport?
  var exclusions: [String] {
    exclusionsText.components(separatedBy: CharacterSet(charactersIn: ",\n"))
      .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }.filter { !$0.isEmpty }
  }
  func acceptPreview(source: String, exclusions: [String], digest: String) -> Bool {
    guard source == self.source, exclusions.sorted() == self.exclusions.sorted(),
          digest.utf8.count == 64,
          digest.utf8.allSatisfy({ (48...57).contains($0) || (97...102).contains($0) }) else {
      confirmed = nil
      return false
    }
    confirmed = ConfirmedImport(source: source, exclusions: exclusions, digest: digest)
    return true
  }
}

// Only unfinished clipboard target metadata is remembered, never text. Each
// configuration keeps its own marker until confirmed success or acknowledgement.
final class ClipboardPending {
  let defaults: UserDefaults
  let key = "PendingClipboardTransfers"
  init(defaults: UserDefaults = .standard) { self.defaults = defaults }
  private var entries: [String: [String: String]] {
    get { defaults.dictionary(forKey: key) as? [String: [String: String]] ?? [:] }
    set { defaults.set(newValue, forKey: key) }
  }
  func request(config: String) -> [String: String]? { entries[config] }
  func begin(config: String, metadata: [String: String]) -> Bool {
    var saved = entries
    guard saved[config] == nil else { return false }
    saved[config] = metadata; entries = saved; return true
  }
  func complete(config: String, requestID: String, succeeded: Bool) {
    guard succeeded, entries[config]?["request"] == requestID else { return }
    acknowledge(config: config)
  }
  func acknowledge(config: String) { var saved = entries; saved.removeValue(forKey: config); entries = saved }
}
