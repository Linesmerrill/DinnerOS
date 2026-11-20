import SwiftUI

/// A recipe in the list: thumbnail, name, headline, and order stats.
struct RecipeRow: View {
    let summary: RecipeSummary

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @ScaledMetric(relativeTo: .body) private var thumbnailWidth = 96.0

    var body: some View {
        let layout =
            dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: DS.Space.s))
            : AnyLayout(HStackLayout(alignment: .top, spacing: DS.Space.m))
        layout {
            RecipeImage(url: summary.imageURL, title: summary.name)
                .frame(width: dynamicTypeSize.isAccessibilitySize ? nil : min(thumbnailWidth, 140))
                .clipShape(.rect(cornerRadius: DS.Radius.small))
            VStack(alignment: .leading, spacing: DS.Space.xxs) {
                Text(summary.name)
                    .font(.headline)
                if let headline = summary.headline, !headline.isEmpty {
                    Text(headline)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                }
                HStack(alignment: .firstTextBaseline, spacing: DS.Space.xs) {
                    if let average = summary.householdRating.average, summary.householdRating.count > 0 {
                        Text("\(Image(systemName: "star.fill")) \(RatingFormat.average(average))")
                            .font(.caption.weight(.semibold))
                            .foregroundStyle(.orange)
                            .accessibilityLabel(RatingFormat.accessibilitySummary(summary.householdRating))
                    }
                    Text(Self.details(for: summary))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .padding(.vertical, DS.Space.xxs)
        .accessibilityElement(children: .combine)
    }

    /// For example "Add-on · 30 min · Ordered 3 times · Last Sep 2026".
    static func details(for summary: RecipeSummary, locale: Locale = .autoupdatingCurrent) -> String {
        var parts: [String] = []
        if summary.isAddon {
            parts.append(String(localized: "Add-on"))
        }
        if let minutes = summary.displayMinutes {
            parts.append(RecipeFormat.minutes(minutes))
        }
        parts.append(RecipeFormat.timesOrdered(summary.timesOrdered))
        if let week = summary.lastOrderedWeek {
            let month = ISOWeek(week)?.monthAndYear(locale: locale) ?? week
            parts.append(String(localized: "Last \(month)"))
        }
        return parts.joined(separator: " · ")
    }
}

/// A recipe photo at a fixed 4:3 aspect ratio, with a placeholder while loading or
/// when there's no image. Thumbnails ask the CDN for a thumbnail-sized photo.
struct RecipeImage: View {
    let url: URL?
    /// The width the photo is shown at, in points.
    var pointWidth: CGFloat = 140
    /// The recipe's name, for the designed picture shown when there's no photo.
    var title: String? = nil

    @Environment(\.displayScale) private var displayScale
    /// The width it's actually drawn at, so a full-width photo (a shared recipe's page) asks
    /// for a full-width image instead of stretching a thumbnail.
    @State private var measuredWidth: CGFloat?

    var body: some View {
        let width = max(pointWidth, measuredWidth ?? 0)
        Rectangle()
            .fill(.quaternary)
            .aspectRatio(4.0 / 3.0, contentMode: .fit)
            .overlay {
                CachedImage(key: ImageKey(url: url, pointWidth: width, scale: displayScale, maxBucket: nil)) {
                    Color.clear
                } fallback: {
                    RecipePlaceholderArt(title: title, showsTitle: width >= 200)
                }
            }
            .clipped()
            .onGeometryChange(for: CGFloat.self) {
                $0.size.width
            } action: { width in
                guard width > 0 else { return }
                measuredWidth = width
            }
            .accessibilityHidden(true)
    }
}

#Preview {
    let session = HouseholdPreviewData.session()
    List(RecipePreviewData.summaries) { summary in
        RecipeRow(summary: summary)
    }
    .listStyle(.plain)
    .environment(RecipePreviewData.library(session: session))
}
