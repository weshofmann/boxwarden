import AppKit

@main struct ProjectStatusIconTests {
  static func main() {
    func check(_ condition: @autoclosure () -> Bool, _ message: String) {
      if !condition() { fputs("FAIL: \(message)\n", stderr); exit(1) }
    }

    let image = ProjectStatusIcon.image()
    check(image.isTemplate, "menu icon is a template so macOS chooses its foreground color")
    check(image.accessibilityDescription == "Boxwarden", "icon has a concise accessibility description")
    check(image.size == NSSize(width: 18, height: 18), "icon uses the native 18 point menu size")

    for pixelsPerPoint in [1, 2] {
      let pixels = pixelsPerPoint * 18
      let bitmap = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: pixels, pixelsHigh: pixels,
        bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
        colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
      bitmap.size = image.size
      NSGraphicsContext.saveGraphicsState()
      NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: bitmap)
      image.draw(in: NSRect(origin: .zero, size: image.size))
      NSGraphicsContext.restoreGraphicsState()

      let content = (0..<pixels).flatMap { y in
        (0..<pixels).compactMap { x -> NSPoint? in
          bitmap.colorAt(x: x, y: y)?.alphaComponent ?? 0 > 0.05 ? NSPoint(x: x, y: y) : nil
        }
      }
      check(!content.isEmpty, "vector drawing renders visible pixels at \(pixels) by \(pixels)")
      let bounds = content.reduce(NSRect.null) { $0.union(NSRect(origin: $1, size: NSSize(width: 1, height: 1))) }
      let margin: CGFloat = 1
      check(bounds.minX >= margin && bounds.minY >= margin && bounds.maxX <= CGFloat(pixels) - margin && bounds.maxY <= CGFloat(pixels) - margin,
        "cube stroke retains a one pixel margin at \(pixels) by \(pixels): \(bounds)")
      check(bounds.width > CGFloat(pixels) * 0.65 && bounds.height > CGFloat(pixels) * 0.65,
        "cube geometry fills most of its \(pixels) pixel canvas")
    }

    print("PASS: menu icon template, accessibility, 18 point geometry and 1x/2x AppKit rendering")
  }
}
