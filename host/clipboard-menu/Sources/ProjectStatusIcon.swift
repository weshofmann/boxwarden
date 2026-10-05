import AppKit

enum ProjectStatusIcon {
  static let size = NSSize(width: 18, height: 18)

  static func image() -> NSImage {
    let image = NSImage(size: size, flipped: false) { rect in
      guard let context = NSGraphicsContext.current else { return false }
      context.saveGraphicsState()
      defer { context.restoreGraphicsState() }

      let path = cubePath(in: rect)
      path.lineWidth = 1.25
      path.lineCapStyle = .round
      path.lineJoinStyle = .round
      NSColor.black.setStroke()
      path.stroke()
      return true
    }
    image.isTemplate = true
    image.accessibilityDescription = "Boxwarden"
    return image
  }

  private static func cubePath(in rect: NSRect) -> NSBezierPath {
    let scaleX = rect.width / size.width
    let scaleY = rect.height / size.height
    func point(_ x: CGFloat, _ y: CGFloat) -> NSPoint {
      NSPoint(x: rect.minX + x * scaleX, y: rect.minY + y * scaleY)
    }

    let top = point(9, 15.5)
    let left = point(2.5, 12)
    let center = point(9, 8.5)
    let right = point(15.5, 12)
    let leftBottom = point(2.5, 4.5)
    let bottom = point(9, 1.5)
    let rightBottom = point(15.5, 4.5)

    let path = NSBezierPath()
    path.move(to: top)
    path.line(to: left)
    path.line(to: center)
    path.line(to: right)
    path.close()

    path.move(to: left)
    path.line(to: leftBottom)
    path.line(to: bottom)
    path.line(to: rightBottom)
    path.line(to: right)

    path.move(to: center)
    path.line(to: bottom)
    return path
  }
}
