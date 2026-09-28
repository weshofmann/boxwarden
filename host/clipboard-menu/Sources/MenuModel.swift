import Foundation

enum ConfigDomains {
  static func names(from data: Data) throws -> [String] {
    guard let object = try JSONSerialization.jsonObject(with: data) as? [String: Any],
          let domains = object["domains"] as? [String: Any] else { throw MenuError.invalidConfiguration }
    let names = domains.keys.sorted()
    guard !names.isEmpty, names.allSatisfy({ MenuTarget.validToken($0) }) else { throw MenuError.invalidConfiguration }
    return names
  }
}

enum MenuError: Error { case invalidConfiguration }

struct MenuTarget: Codable, Equatable {
  let domain: String
  let session: String
  let sessionID: String
  let backendKind: String
  let backendObject: String
  let generation: String
  var available: Bool

  enum CodingKeys: String, CodingKey {
    case domain, session, generation, available
    case sessionID = "session_id"
    case backendKind = "backend_kind"
    case backendObject = "backend_object"
  }
  var key: String { domain + "/" + session }
  var isValid: Bool {
    Self.validToken(domain) && Self.validToken(session) && Self.validToken(backendObject) && backendKind == "tart" &&
    UUID(uuidString: sessionID) != nil && (!available || UUID(uuidString: generation) != nil)
  }
  static func validToken(_ text: String) -> Bool {
    !text.isEmpty && text.utf8.count <= 255 && text.first != "-" && text.unicodeScalars.allSatisfy {
      CharacterSet(charactersIn: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_.-").contains($0)
    }
  }
}

enum TransferDirection: String { case push, pull }
enum TransferResult { case success, failed, cancelled }

struct TransferRequest {
  let id: UUID
  let target: MenuTarget
  let direction: TransferDirection
  func arguments(config: String) -> [String] {
    ["--config", config, "--domain", target.domain, "clipboard", direction.rawValue, target.session,
     "--expected-session-id", target.sessionID, "--expected-backend-kind", target.backendKind,
     "--expected-backend-object", target.backendObject, "--expected-generation", target.generation]
  }
}

final class MenuState {
  private(set) var targets: [MenuTarget] = []
  private(set) var selected: MenuTarget?
  private(set) var busy = false
  private(set) var refreshing = false
  private(set) var status = "Ready."
  private var refreshID: UUID?
  private var transferID: UUID?

  var canTransfer: Bool { !busy && !refreshing && (selected?.available == true) && (selected?.isValid == true) }

  @discardableResult func beginRefresh() -> UUID {
    let id = UUID()
    refreshID = id
    refreshing = true
    targets = targets.map { var target = $0; target.available = false; return target }
    if let selected { var stale = selected; stale.available = false; self.selected = stale }
    return id
  }
  func acceptRefresh(_ id: UUID, targets: [MenuTarget], failedDomains: Int = 0) {
    guard refreshID == id else { return }
    refreshID = nil
    refreshing = false
    let prior = selected
    self.targets = targets.filter(\.isValid).sorted { $0.key < $1.key }
    selected = prior.flatMap { old in self.targets.first {
      $0.domain == old.domain && $0.session == old.session && $0.sessionID == old.sessionID &&
      $0.backendKind == old.backendKind && $0.backendObject == old.backendObject && $0.generation == old.generation
    } }
    if failedDomains > 0 { status = "Some sandbox status unavailable." }
    else if self.targets.isEmpty { status = "No configured sandboxes found." }
  }
  func failRefresh(_ id: UUID) {
    guard refreshID == id else { return }
    refreshID = nil
    refreshing = false
    targets = []
    selected = nil
    status = "Sandbox status unavailable."
  }
  func select(_ target: MenuTarget) {
    selected = targets.first { $0 == target }
  }
  func beginTransfer(_ direction: TransferDirection) -> TransferRequest? {
    guard canTransfer, let target = selected else { return nil }
    let request = TransferRequest(id: UUID(), target: target, direction: direction)
    transferID = request.id
    busy = true
    status = "Transfer in progress."
    return request
  }
  func finishTransfer(_ id: UUID, result: TransferResult) {
    guard transferID == id else { return }
    transferID = nil
    busy = false
    switch result {
    case .success: status = "Transfer completed."
    case .failed: status = "Transfer failed; outcome unknown."
    case .cancelled: status = "Transfer cancelled; outcome unknown."
    }
  }
}

struct TargetResponse: Decodable { let targets: [MenuTarget] }
