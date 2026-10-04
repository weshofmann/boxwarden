import Foundation

enum ProjectCommand: Equatable {
  case list, create(name: String, recipe: String, sizeMiB: Int)
  case open(name: String), status(name: String), stop(name: String)
  case importPreview(source: String, exclusions: [String])
  case importProject(name: String, source: String, exclusions: [String], previewDigest: String)
  case importRetry(name: String), export(name: String, destination: String)
  case exportList(name: String), exportRetry(name: String, transaction: String)
  case rebuild(name: String, recipe: String), rebuildRetry(name: String)

  var operation: String {
    switch self {
    case .list: return "project.list"
    case .create: return "project.create"
    case .open: return "project.open"
    case .status: return "project.status"
    case .stop: return "project.stop"
    case .importPreview: return "project.import.preview"
    case .importProject: return "project.import"
    case .importRetry: return "project.import.retry"
    case .export: return "project.export"
    case .exportList: return "project.export.list"
    case .exportRetry: return "project.export.retry"
    case .rebuild: return "project.rebuild"
    case .rebuildRetry: return "project.rebuild.retry"
    }
  }
  var projectName: String? {
    switch self {
    case .list, .importPreview: return nil
    case .create(let n, _, _), .open(let n), .status(let n), .stop(let n),
         .importProject(let n, _, _, _), .importRetry(let n), .export(let n, _),
         .exportList(let n), .exportRetry(let n, _), .rebuild(let n, _), .rebuildRetry(let n): return n
    }
  }
  var isMutation: Bool {
    switch self { case .list, .status, .importPreview, .exportList: return false; default: return true }
  }
  func arguments(config: String, domain: String) throws -> [String] {
    guard Self.validPath(config), Self.validToken(domain), projectName.map(Self.validToken) ?? true else {
      throw ProjectClientError.invalidRequest("Invalid configuration, domain, or project locator")
    }
    let prefix = ["--config", config, "--domain", domain, "project"]
    var args: [String]
    switch self {
    case .list: args = ["list", "--json"]
    case .create(let n, let recipe, let size):
      guard ["desktop", "actions", "chatgpt"].contains(recipe), size > 0 else { throw ProjectClientError.invalidRequest("Invalid recipe or workspace size") }
      args = ["create", "--json", "--recipe", recipe, "--size-mib", String(size), n]
    case .open(let n): args = ["open", "--json", n]
    case .status(let n): args = ["status", "--json", n]
    case .stop(let n): args = ["stop", "--json", n]
    case .importPreview(let source, let exclusions): args = ["import", "preview", "--json"] + (try Self.sourceArguments(source, exclusions))
    case .importProject(let n, let source, let exclusions, let digest):
      guard digest.count == 64, digest.allSatisfy({ "0123456789abcdef".contains($0) }) else { throw ProjectClientError.invalidRequest("Invalid preview digest") }
      args = ["import", "--json"] + (try Self.sourceArguments(source, exclusions)) + ["--expected-digest", digest, n]
    case .importRetry(let n): args = ["import", "retry", "--json", n]
    case .export(let n, let destination):
      guard Self.validPath(destination) else { throw ProjectClientError.invalidRequest("Invalid export destination") }
      args = ["export", "--json", "--destination", destination, n]
    case .exportList(let n): args = ["export", "list", "--json", n]
    case .exportRetry(let n, let transaction):
      guard UUID(uuidString: transaction) != nil else { throw ProjectClientError.invalidRequest("Invalid export transaction") }
      args = ["export", "retry", "--json", "--transaction", transaction, n]
    case .rebuild(let n, let recipe):
      guard ["desktop", "actions", "chatgpt"].contains(recipe) else { throw ProjectClientError.invalidRequest("Invalid recipe") }
      args = ["rebuild", "--json", "--recipe", recipe, n]
    case .rebuildRetry(let n): args = ["rebuild", "retry", "--json", n]
    }
    return prefix + args
  }
  private static func sourceArguments(_ source: String, _ exclusions: [String]) throws -> [String] {
    guard validPath(source), exclusions.count <= 256,
          exclusions.allSatisfy({ !$0.isEmpty && $0.utf8.count <= 4096 && !$0.unicodeScalars.contains(where: { CharacterSet.controlCharacters.contains($0) }) }) else {
      throw ProjectClientError.invalidRequest("Invalid source or exclusions")
    }
    return ["--source", source] + exclusions.flatMap { ["--exclude", $0] }
  }
  static func validToken(_ value: String) -> Bool {
    !value.isEmpty && value.utf8.count <= 128 && value.utf8.allSatisfy { (48...57).contains($0) || (65...90).contains($0) || (97...122).contains($0) || $0 == 45 || $0 == 95 || $0 == 46 } && value != "." && value != ".." && !value.hasPrefix("-")
  }
  static func validPath(_ value: String) -> Bool {
    value.hasPrefix("/") && value.utf8.count <= 4096 && !value.unicodeScalars.contains(where: { CharacterSet.controlCharacters.contains($0) }) && !value.contains("//") && !value.split(separator: "/").contains(where: { $0 == "." || $0 == ".." }) && (value == "/" || !value.hasSuffix("/"))
  }
}

struct ProjectSetup: Decodable { let status: String; let guidance: String }
struct ProjectWorkspace: Decodable { let id: String; let filesystemUuid: String; let sizeBytes: Int64; let mountPath: String; let initialized: Bool }
struct ProjectSoftwareAction: Decodable { let actionId: String; let phase: String; let state: String; let generation: String }
struct ProjectSoftware: Decodable { let intentDigest: String; let status: String; let actions: [ProjectSoftwareAction] }
struct ProjectImport: Decodable { let status: String; let id: String; let guestPath: String }
struct ProjectRecord: Decodable {
  let name: String; let base: String; let sessionId: String; let backendObject: String
  let state: String; let managementReady: Bool; let diagnostic: String
  let observedState: String?; let backendRunning: Bool?
  let workspace: ProjectWorkspace; let software: ProjectSoftware; let importState: ProjectImport
  let replacementPending: Bool; let availableActions: [String]
  enum CodingKeys: String, CodingKey {
    case name, base, sessionId, backendObject, state, managementReady, diagnostic, observedState, backendRunning, workspace, software
    case importState = "import"
    case replacementPending, availableActions
  }
}
struct ProjectList: Decodable { let setup: ProjectSetup; let projects: [ProjectRecord] }
struct ProjectPreviewEntry: Decodable { let path: String; let kind: String; let size: Int64?; let sha256: String? }
struct ProjectPreview: Decodable {
  let source: String; let entries: [ProjectPreviewEntry]; let fileCount: Int; let directoryCount: Int
  let totalBytes: Int64; let digest: String; let exclusions: [String]
}
struct ProjectExportReceipt: Decodable { let transaction: String; let phase: String; let published: String; let projectFiles: String }
struct ProjectExportEntry: Decodable {
  let transaction: String; let phase: String; let destinationParent: String; let published: String; let projectFiles: String
  let matchesCurrentBookmark: Bool; let retryAvailable: Bool
}
struct ProjectExportList: Decodable { let projectName: String; let exports: [ProjectExportEntry] }
enum ProjectResponse { case list(ProjectList), project(ProjectRecord), preview(ProjectPreview), export(ProjectExportReceipt), exports(ProjectExportList) }

struct ProjectEvent: Decodable {
  let version: Int; let type: String; let operation: String; let message: String?; let response: ProjectResponse?; let uncertain: Bool
  private enum CodingKeys: String, CodingKey { case version, type, operation, message, data }
  private struct ProjectData: Decodable { let project: ProjectRecord }
  private struct ErrorData: Decodable { let uncertain: Bool? }
  init(from decoder: Decoder) throws {
    let c = try decoder.container(keyedBy: CodingKeys.self)
    version = try c.decode(Int.self, forKey: .version)
    type = try c.decode(String.self, forKey: .type)
    operation = try c.decode(String.self, forKey: .operation)
    message = try c.decodeIfPresent(String.self, forKey: .message)
    guard version == 1, ["progress", "result", "error"].contains(type) else { throw ProjectClientError.invalidResponse("Unsupported JSON event") }
    uncertain = type == "error" ? (try c.decodeIfPresent(ErrorData.self, forKey: .data)?.uncertain ?? true) : false
    if type == "result" {
      switch operation {
      case "project.list": response = .list(try c.decode(ProjectList.self, forKey: .data))
      case "project.import.preview": response = .preview(try c.decode(ProjectPreview.self, forKey: .data))
      case "project.export", "project.export.retry": response = .export(try c.decode(ProjectExportReceipt.self, forKey: .data))
      case "project.export.list": response = .exports(try c.decode(ProjectExportList.self, forKey: .data))
      case "project.create", "project.open", "project.status", "project.stop", "project.import", "project.import.retry", "project.rebuild", "project.rebuild.retry": response = .project(try c.decode(ProjectData.self, forKey: .data).project)
      default: throw ProjectClientError.invalidResponse("Unknown result operation")
      }
    } else { response = nil }
  }
  static func decode(_ data: Data, operation: String) throws -> ProjectEvent {
    let decoder = JSONDecoder(); decoder.keyDecodingStrategy = .convertFromSnakeCase
    let event = try decoder.decode(ProjectEvent.self, from: data)
    guard event.operation == operation else { throw ProjectClientError.invalidResponse("JSON operation does not match request") }
    return event
  }
}

enum ProjectOutcome {
  case success(ProjectResponse), failed(String), unknown(String)
  var message: String {
    switch self { case .success: return "Completed"; case .failed(let m), .unknown(let m): return m }
  }
}
enum ProjectClientError: Error, LocalizedError {
  case invalidRequest(String), invalidResponse(String), activity(String), busy(String), process(String)
  var errorDescription: String? {
    switch self { case .invalidRequest(let m), .invalidResponse(let m), .activity(let m), .busy(let m), .process(let m): return m }
  }
}
