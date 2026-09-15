import SwiftUI

/// A recipe in the list: thumbnail, name, headline, and order stats.
struct RecipeRow: View {
    let summary: RecipeSummary

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @ScaledMetric(relativeTo: .body) private var thumbnailWidth = 96.0

    var body: some View {
        let layout =
            dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 8))
            : AnyLayout(HStackLayout(alignment: .top, spacing: 12))
        layout {
            RecipeImage(url: summary.imageURL)
                .frame(width: dynamicTypeSize.isAccessibilitySize ? nil : min(thumbnailWidth, 140))
                .clipShape(.rect(cornerRadius: 8))
            VStack(alignment: .leading, spacing: 4) {
                Text(summary.name)
                    .font(.headline)
                if let headline = summary.headline, !headline.isEmpty {
                    Text(headline)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                }
                HStack(alignment: .firstTextBaseline, spacing: 6) {
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
        .padding(.vertical, 4)
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

    @Environment(\.displayScale) private var displayScale

    var body: some View {
        Rectangle()
            .fill(.quaternary)
            .aspectRatio(4.0 / 3.0, contentMode: .fit)
            .overlay {
                let sized = RecipeImageURL.sized(url, pointWidth: pointWidth, scale: displayScale)
                AsyncImage(url: sized, transaction: Transaction(animation: .easeIn(duration: 0.2))) { phase in
                    if let image = phase.image {
                        image
                            .resizable()
                            .scaledToFill()
                    } else {
                        Image(systemName: "fork.knife")
                            .font(.title2)
                            .foregroundStyle(.secondary)
                    }
                }
            }
            .clipped()
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
