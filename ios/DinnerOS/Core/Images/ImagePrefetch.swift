import CoreGraphics
import Foundation
import SwiftUI

/// Which photos to warm when a card appears.
///
/// A carousel or list row starts the next few cards' photos so they're decoded by the time
/// they're scrolled to. The window is small on purpose: warming a whole section would compete
/// with the photos actually on screen, which is the stutter this is meant to remove.
nonisolated enum ImagePrefetchWindow {
    /// Cards warmed ahead of the one that just appeared.
    static let count = 5

    /// Keys for the `count` photos after `index`, skipping recipes with no photo.
    static func keys(after index: Int, in urls: [URL?], pointWidth: CGFloat, scale: CGFloat) -> [ImageKey] {
        guard index >= 0, !urls.isEmpty else { return [] }
        let start = min(index + 1, urls.count)
        let end = min(start + count, urls.count)
        guard start < end else { return [] }
        return urls[start..<end].compactMap { ImageKey(url: $0, pointWidth: pointWidth, scale: scale) }
    }
}

extension View {
    /// Warms the photos after `index` while this card is on screen, and stops when it leaves.
    ///
    /// Attached to the card rather than the section, so a section that is never scrolled to
    /// never warms anything.
    func prefetchesPhotos(after index: Int, in urls: [URL?], pointWidth: CGFloat) -> some View {
        modifier(PhotoPrefetchModifier(index: index, urls: urls, pointWidth: pointWidth))
    }
}

private struct PhotoPrefetchModifier: ViewModifier {
    let index: Int
    let urls: [URL?]
    let pointWidth: CGFloat

    @Environment(\.displayScale) private var displayScale
    @Environment(\.imageLoader) private var loader

    func body(content: Content) -> some View {
        content.task(id: index) {
            let keys = ImagePrefetchWindow.keys(
                after: index, in: urls, pointWidth: pointWidth, scale: displayScale)
            guard !keys.isEmpty else { return }
            await loader.prefetch(keys)
            // Scrolling the card away cancels this task; the warmed fetches are then released
            // and any that nothing is waiting for stop.
            await withTaskCancellationHandler {
                // Stays resident while the card is on screen so the cancellation handler runs
                // when it leaves.
                try? await Task.sleep(for: .seconds(60))
            } onCancel: {
                Task { await loader.cancelPrefetch(keys) }
            }
        }
    }
}
