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
    /// The recipe's photo at the top of the cooking screen, where it sits, so a test can check its
    /// pixels: a photo drawn invisible left a blank band there on TestFlight 120.
    static let cookPhoto = "cook.photo"
    /// An ingredient row on the cooking screen's list (an identifier on the row itself).
    static let cookIngredient = "cook.ingredient"

    /// The whole of a Your Meals card, held space included: a card's own accessibility frame
    /// covers only what VoiceOver reads, so the line-up check measures this instead.
    static let mealCardFrame = "menu.mealCardFrame"

    /// By Day's day headings ("byDay.day.sun") and the heading over meals without a day, so a
    /// test can check every day of the week shows, in order, below the week strip.
    static let byDayHeaderPrefix = "byDay.day."
    static let byDayUnscheduledHeader = "byDay.unscheduled"
    /// The week strip under the Menu's toolbar.
    static let weekStrip = "menu.weekStrip"
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

// MARK: Covers

/// What draws over the app's own scrolling content right now: the fades at pinned bars' edges
/// (`DSEdgeFade`, the tab bar's scrim) and the solid bars they hang from. The UI tests check that
/// a screen's rows rest clear of the fades and never part way under a bar (Hark's review, build
/// 121 round 2: Sign Out washed out above the tab bar, a recipe's first Nutrition row under the
/// pinned picker).
///
/// Reported by frame, not as elements over the screen, so a marker never sits over a button a
/// test taps: one 1-point element at the window's corner (`qaCoversElement`) lists them all in
/// its value, in screen points, as "f:x,y,w,h;b:x,y,w,h" (`f` a fade, `b` a bar). Off without
/// `QA_MARKERS=1`.
@MainActor @Observable final class QACovers {
    nonisolated enum Kind: String, Sendable {
        case fade = "f"
        case bar = "b"
    }

    static let shared = QACovers()
    /// The identifier of the element that lists the covers.
    nonisolated static let identifier = "qa.covers"

    private var covers: [UUID: (kind: Kind, frame: CGRect)] = [:]

    func set(_ id: UUID, _ kind: Kind, _ frame: CGRect?) {
        guard let frame, frame.width >= 1, frame.height >= 1 else {
            if covers[id] != nil { covers[id] = nil }
            return
        }
        if covers[id]?.frame != frame || covers[id]?.kind != kind { covers[id] = (kind, frame) }
    }

    /// Every cover on screen, as the element's value.
    var value: String {
        QACovers.encode(Array(covers.values))
    }

    /// "k:x,y,w,h" for each cover, rounded to a tenth of a point, top to bottom, ";" between.
    nonisolated static func encode(_ covers: [(kind: Kind, frame: CGRect)]) -> String {
        covers.sorted { ($0.frame.minY, $0.frame.minX) < ($1.frame.minY, $1.frame.minX) }
            .map { cover in
                let frame = cover.frame
                return cover.kind.rawValue + ":"
                    + [frame.minX, frame.minY, frame.width, frame.height]
                    .map { String(format: "%.1f", Double($0)) }
                    .joined(separator: ",")
            }
            .joined(separator: ";")
    }
}

private struct QACoverReporter: ViewModifier {
    let kind: QACovers.Kind
    /// Where a bar pinned by `stickyBelowTop` stops (it moves only its drawing, which its frame
    /// doesn't show), and how tall the fade hanging under it is: reported as a fade of its own.
    var pinnedAt: CGFloat?
    var fade: CGFloat = 0
    @State private var id = UUID()
    @State private var fadeID = UUID()
    @State private var frame = CGRect.null

    private func report() {
        QACovers.shared.set(id, kind, frame.isNull ? nil : frame)
        QACovers.shared.set(
            fadeID, .fade,
            fade > 0 && !frame.isNull ? CGRect(x: frame.minX, y: frame.maxY, width: frame.width, height: fade) : nil)
    }

    func body(content: Content) -> some View {
        content
            .onGeometryChange(for: CGRect.self) { [pinnedAt] proxy in
                var frame = proxy.frame(in: .global)
                if let pinnedAt {
                    frame.origin.y += RecipeDetailHeader.stickyOffset(
                        minY: proxy.frame(in: .scrollView).minY, top: pinnedAt)
                }
                return frame
            } action: { newFrame in
                frame = newFrame
                report()
            }
            // The fade grows and shrinks while the bar stays put.
            .onChange(of: fade) { report() }
            .onDisappear {
                frame = .null
                report()
            }
    }
}

private struct QACoversElement: View {
    var body: some View {
        Color.clear
            .frame(width: 1, height: 1)
            .accessibilityElement()
            .accessibilityIdentifier(QACovers.identifier)
            .accessibilityLabel("Covers")
            .accessibilityValue(QACovers.shared.value)
            .allowsHitTesting(false)
    }
}

extension View {
    /// Reports this view's frame as one that covers the content scrolling under it (`QACovers`):
    /// a fade, or a pinned bar. Nothing at all without `QA_MARKERS=1`.
    @ViewBuilder
    func qaCover(_ kind: QACovers.Kind) -> some View {
        if QAMarkers.isOn {
            modifier(QACoverReporter(kind: kind))
        } else {
            self
        }
    }

    /// The same for a bar pinned with `stickyBelowTop(pinnedAt)`, with the fade `fade` tall
    /// that hangs under it.
    @ViewBuilder
    func qaCover(pinnedAt: CGFloat, fade: CGFloat) -> some View {
        if QAMarkers.isOn {
            modifier(QACoverReporter(kind: .bar, pinnedAt: pinnedAt, fade: max(fade, 0)))
        } else {
            self
        }
    }

    /// The element listing every cover (`QACovers`), at the window's top corner, on the app's
    /// root. Nothing at all without `QA_MARKERS=1`.
    @ViewBuilder
    func qaCoversElement() -> some View {
        if QAMarkers.isOn {
            overlay(alignment: .topLeading) {
                QACoversElement()
                    .ignoresSafeArea()
            }
        } else {
            self
        }
    }
}
