import SwiftUI

/// Marks for the UI tests on what a screenshot alone can't tell apart: a photo that fell back to
/// its placeholder art. The photos are hidden from VoiceOver, so the tests can't see them either;
/// a marker is an empty element beside the photo that they can find by its identifier.
///
/// Debug builds only, and only when the UI tests launch the app with `QA_MARKERS=1`
/// (`launchSignedIn`), so nobody using the app, VoiceOver included, ever meets one.
nonisolated enum QAMarkers {
    static let isOn: Bool = {
        #if DEBUG
            ProcessInfo.processInfo.environment["QA_MARKERS"] == "1"
        #else
            false
        #endif
    }()

    /// A recipe or ingredient photo showing its no-photo art.
    static let photoPlaceholder = "photo.placeholder"
}

extension View {
    /// An empty element with `identifier` (and `label`, naming the item) over this view while
    /// `isShown`, for the UI tests. Nothing at all without `QA_MARKERS=1`.
    @ViewBuilder
    func qaMarker(_ identifier: String, label: String, isShown: Bool) -> some View {
        if QAMarkers.isOn {
            overlay {
                if isShown {
                    Color.clear
                        .accessibilityElement()
                        .accessibilityIdentifier(identifier)
                        .accessibilityLabel(label)
                }
            }
        } else {
            self
        }
    }
}
