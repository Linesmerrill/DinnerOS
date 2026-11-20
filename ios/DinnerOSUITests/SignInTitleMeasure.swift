import UIKit
import XCTest

/// Measures a sign-in button's title as drawn (build 121): the height, in points, of the first
/// letter after the icon slot. "Continue with Apple", "Continue with Google" and "Sign in with
/// Face ID" all start with a round capital (C, C, S), so the same font gives the same height and a
/// larger font a taller one (22 pt against 20 pt is about 1.4 pt taller).
enum SignInTitleMeasure {
    /// nil when no letter could be found (the button isn't on screen).
    static func firstLetterHeight(of frame: CGRect, in upright: UIImage, screen: CGRect) -> CGFloat? {
        guard let image = upright.cgImage, !frame.isEmpty else { return nil }
        let scale = CGFloat(image.width) / screen.width
        guard let pixels = Pixels(image) else { return nil }
        let rect = CGRect(
            x: (frame.minX - screen.minX) * scale, y: (frame.minY - screen.minY) * scale,
            width: frame.width * scale, height: frame.height * scale
        ).integral
        // The fill, just inside the capsule's left end, on its middle row.
        let midY = Int(rect.midY)
        let background = pixels.color(x: Int(rect.minX + frame.height * 0.5 * scale), y: midY)
        // Leave out the capsule's rounded ends and its outline.
        let left = Int(rect.minX + frame.height * 0.6 * scale)
        let right = Int(rect.maxX - frame.height * 0.6 * scale)
        let top = Int(rect.minY + 3 * scale)
        let bottom = Int(rect.maxY - 3 * scale)
        guard left < right, top < bottom else { return nil }
        func ink(_ x: Int, _ y: Int) -> Bool { pixels.color(x: x, y: y).distance(to: background) > 0.35 }
        func columnHasInk(_ x: Int) -> Bool { (top...bottom).contains { ink(x, $0) } }

        // The icon slot starts at the first ink; the title's first letter is the first ink after
        // the slot (20 pt at a 50 pt button) and the gap after it.
        guard let iconStart = (left...right).first(where: columnHasInk) else { return nil }
        let slot = Int(frame.height * 0.4 * scale) + Int(2 * scale)
        var x = iconStart + slot
        while x < right, columnHasInk(x) { x += 1 }  // the end of the icon, if it runs on
        guard let letterStart = (x...right).first(where: columnHasInk) else { return nil }
        var letterEnd = letterStart
        while letterEnd + 1 < right, columnHasInk(letterEnd + 1) { letterEnd += 1 }
        var minY = bottom
        var maxY = top
        for column in letterStart...letterEnd {
            for row in top...bottom where ink(column, row) {
                minY = min(minY, row)
                maxY = max(maxY, row)
            }
        }
        guard maxY > minY else { return nil }
        return CGFloat(maxY - minY + 1) / scale
    }

    private struct RGB {
        let r: CGFloat
        let g: CGFloat
        let b: CGFloat
        func distance(to other: RGB) -> CGFloat {
            max(abs(r - other.r), abs(g - other.g), abs(b - other.b))
        }
    }

    private struct Pixels {
        let data: [UInt8]
        let width: Int
        let height: Int

        init?(_ image: CGImage) {
            width = image.width
            height = image.height
            var buffer = [UInt8](repeating: 0, count: width * height * 4)
            guard
                let context = CGContext(
                    data: &buffer, width: width, height: height, bitsPerComponent: 8, bytesPerRow: width * 4,
                    space: CGColorSpaceCreateDeviceRGB(),
                    bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)
            else { return nil }
            context.draw(image, in: CGRect(x: 0, y: 0, width: width, height: height))
            data = buffer
        }

        func color(x: Int, y: Int) -> RGB {
            let cx = min(max(x, 0), width - 1)
            let cy = min(max(y, 0), height - 1)
            let i = (cy * width + cx) * 4
            return RGB(r: CGFloat(data[i]) / 255, g: CGFloat(data[i + 1]) / 255, b: CGFloat(data[i + 2]) / 255)
        }
    }
}
