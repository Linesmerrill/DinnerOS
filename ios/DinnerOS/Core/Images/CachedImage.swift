import SwiftUI
import UIKit

/// What a photo view draws while it waits, fails, or succeeds.
///
/// Split out from the view so the rules are testable: a recipe with no photo shows the glyph
/// immediately rather than a spinner that never resolves, a failed load settles on the glyph
/// instead of retrying forever, and a placeholder only appears once a load has taken long
/// enough to be worth admitting to.
nonisolated enum ImagePresentation {
    enum State: Equatable {
        /// The decoded photo.
        case image
        /// The shimmer: this load is slow enough to show something.
        case placeholder
        /// The glyph: there's no photo, or it couldn't be loaded.
        case fallback
        /// Nothing yet — a plain background for the first instant of a load.
        case empty
    }

    /// How long a load runs before it admits to being a load. Short enough not to feel stuck,
    /// long enough that a cached photo or a fast fetch never flashes a placeholder.
    static let placeholderDelay = Duration.milliseconds(100)

    static func state(hasImage: Bool, hasKey: Bool, didFail: Bool, delayElapsed: Bool) -> State {
        if hasImage { return .image }
        // A recipe with no image at all never loads anything, so it shows the glyph at once.
        if !hasKey || didFail { return .fallback }
        return delayElapsed ? .placeholder : .empty
    }
}

/// A photo from `ImageLoader`, drawn to fill its frame.
///
/// The view owns nothing but the phase: the cache, coalescing and cancellation all live in the
/// loader. Leaving the screen cancels the `task`, which releases this view's claim on the
/// fetch.
struct CachedImage<Placeholder: View, Fallback: View>: View {
    let key: ImageKey?
    @ViewBuilder var placeholder: Placeholder
    @ViewBuilder var fallback: Fallback

    @Environment(\.imageLoader) private var loader
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    @State private var image: UIImage?
    @State private var didFail = false
    @State private var delayElapsed = false

    private var state: ImagePresentation.State {
        ImagePresentation.state(
            hasImage: image != nil, hasKey: key != nil, didFail: didFail, delayElapsed: delayElapsed)
    }

    var body: some View {
        content
            .animation(reduceMotion ? nil : .easeIn(duration: 0.2), value: image == nil)
            .task(id: key) { await load() }
    }

    @ViewBuilder
    private var content: some View {
        switch state {
        case .image:
            if let image {
                Image(uiImage: image)
                    .resizable()
                    .scaledToFill()
            }
        case .placeholder:
            placeholder
        case .fallback:
            fallback
        case .empty:
            Color.clear
        }
    }

    private func load() async {
        guard let key else {
            image = nil
            didFail = false
            delayElapsed = false
            return
        }
        // An already-decoded photo is drawn on this frame, with no placeholder in between.
        if let cached = await loader.cachedImage(for: key) {
            image = cached
            didFail = false
            delayElapsed = false
            return
        }
        image = nil
        didFail = false
        delayElapsed = false
        let timer = Task { @MainActor in
            try? await Task.sleep(for: ImagePresentation.placeholderDelay)
            guard !Task.isCancelled else { return }
            delayElapsed = true
        }
        defer { timer.cancel() }
        let loaded = await loader.image(for: key)
        guard !Task.isCancelled else { return }
        image = loaded
        didFail = loaded == nil
    }
}

extension EnvironmentValues {
    /// The loader photo views use. Tests and previews substitute one with no network.
    @Entry var imageLoader: ImageLoader = .shared
}
