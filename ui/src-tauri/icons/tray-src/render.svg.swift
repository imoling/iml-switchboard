import AppKit
// 用法：render <svg> <png> <px> [<svg> <png> <px> ...]
var a = Array(CommandLine.arguments.dropFirst())
while a.count >= 3 {
    let svg = a[0], out = a[1], px = Int(a[2])!
    a.removeFirst(3)
    guard let img = NSImage(contentsOfFile: svg) else { fputs("读不了 \(svg)\n", stderr); exit(1) }
    let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: px, pixelsHigh: px, bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
    rep.size = NSSize(width: px, height: px)
    NSGraphicsContext.saveGraphicsState()
    let ctx = NSGraphicsContext(bitmapImageRep: rep)!
    NSGraphicsContext.current = ctx
    ctx.imageInterpolation = .high
    img.draw(in: NSRect(x: 0, y: 0, width: px, height: px), from: .zero, operation: .sourceOver, fraction: 1)
    NSGraphicsContext.restoreGraphicsState()
    let data = rep.representation(using: .png, properties: [:])!
    try! data.write(to: URL(fileURLWithPath: out))
    print("写出 \(out) \(px)px")
}
