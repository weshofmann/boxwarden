import XCTest
import Foundation
@testable import tart

final class ControlledClipboardRunTests: XCTestCase {
  // Argument validation requires existing file names. These empty disposable
  // files are compile/parser fixtures, not bootable images or backend VMs.
  private func withFixture(_ body: () throws -> Void) throws {
    let temporary = FileManager.default.temporaryDirectory.appendingPathComponent("bw-parse-" + UUID().uuidString)
    let directory = temporary.appendingPathComponent("vms/unbound-synthetic")
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    for name in ["config.json", "disk.img", "nvram.bin"] {
      try Data().write(to: directory.appendingPathComponent(name))
    }
    let previous = getenv("TART_HOME").map { String(cString: $0) }
    setenv("TART_HOME", temporary.path, 1)
    defer {
      if let previous = previous { setenv("TART_HOME", previous, 1) } else { unsetenv("TART_HOME") }
      try? FileManager.default.removeItem(at: temporary)
    }
    try body()
  }
  func testUnboundViewerCannotEnableAutomaticClipboard() throws {
    try withFixture {
      let run = try Run.parse(["unbound-synthetic"])
      XCTAssertTrue(run.noClipboard, "unbound viewer must also disable automatic SPICE clipboard")
      XCTAssertNil(try run.clipboardTarget())
    }
  }
  func testExplicitNoClipboardKeepsUnboundViewerDisabled() throws {
    try withFixture {
      XCTAssertTrue(try Run.parse(["--no-clipboard", "unbound-synthetic"]).noClipboard)
    }
  }
}
