import SwiftUI
import UIKit
import WidgetKit

// MARK: - Timeline

struct DinnerWeekEntry: TimelineEntry {
    let date: Date
    let snapshot: DinnerWidgetSnapshot?
    /// Downloaded photos by recipe ID, already shrunk for a widget.
    let photos: [String: UIImage]

    var tonight: DinnerWidgetSnapshot.Meal? { snapshot?.meals(on: date).first }
    var tonightAddons: [DinnerWidgetSnapshot.Meal] { Array((snapshot?.meals(on: date) ?? []).dropFirst()) }
    var upcoming: [(date: String, meal: DinnerWidgetSnapshot.Meal)] { snapshot?.upcoming(after: date) ?? [] }
}

struct DinnerWeekProvider: TimelineProvider {
    func placeholder(in context: Context) -> DinnerWeekEntry {
        DinnerWeekEntry(date: .now, snapshot: .preview, photos: [:])
    }

    func getSnapshot(in context: Context, completion: @escaping (DinnerWeekEntry) -> Void) {
        let snapshot = context.isPreview ? (DinnerWidgetStore.load() ?? .preview) : DinnerWidgetStore.load()
        let family = context.family
        // WidgetKit calls back on any thread; the entry is built and handed over once.
        nonisolated(unsafe) let completion = completion
        Task { completion(await Self.entry(for: .now, snapshot: snapshot, family: family)) }
    }

    func getTimeline(in context: Context, completion: @escaping (Timeline<DinnerWeekEntry>) -> Void) {
        let snapshot = DinnerWidgetStore.load()
        let family = context.family
        nonisolated(unsafe) let completion = completion
        Task {
            let now = Date.now
            let calendar = snapshot?.calendar ?? .current
            // "Tonight" changes at midnight: one entry now, one at the start of tomorrow.
            let midnight = calendar.startOfDay(for: calendar.date(byAdding: .day, value: 1, to: now) ?? now)
            let today = await Self.entry(for: now, snapshot: snapshot, family: family)
            let tomorrow = await Self.entry(for: midnight, snapshot: snapshot, family: family)
            completion(Timeline(entries: [today, tomorrow], policy: .after(midnight)))
        }
    }

    private static func entry(for date: Date, snapshot: DinnerWidgetSnapshot?, family: WidgetFamily) async
        -> DinnerWeekEntry
    {
        guard let snapshot else { return DinnerWeekEntry(date: date, snapshot: nil, photos: [:]) }
        var wanted: [DinnerWidgetSnapshot.Meal] = []
        if let tonight = snapshot.meals(on: date).first { wanted.append(tonight) }
        if family == .systemMedium || family == .systemLarge {
            wanted += snapshot.upcoming(after: date).prefix(4).map(\.meal)
        }
        var photos: [String: UIImage] = [:]
        await withTaskGroup(of: (String, UIImage?).self) { group in
            for meal in wanted where family != .accessoryInline && family != .accessoryRectangular {
                guard let string = meal.imageURL, let url = URL(string: string) else { continue }
                let side: CGFloat = meal.recipeID == wanted.first?.recipeID ? 700 : 160
                group.addTask { (meal.recipeID, await photo(url, side: side)) }
            }
            for await (id, image) in group { if let image { photos[id] = image } }
        }
        return DinnerWeekEntry(date: date, snapshot: snapshot, photos: photos)
    }

    /// A photo shrunk to `side` points square: widgets have little memory to spare.
    private static func photo(_ url: URL, side: CGFloat) async -> UIImage? {
        guard let (data, _) = try? await URLSession.shared.data(from: url), let image = UIImage(data: data) else {
            return nil
        }
        let scale = side / min(image.size.width, image.size.height)
        let size = CGSize(width: image.size.width * scale, height: image.size.height * scale)
        return image.preparingThumbnail(of: size) ?? image
    }
}

// MARK: - Widget

struct DinnerWeekWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "DinnerWeek", provider: DinnerWeekProvider()) { entry in
            DinnerWeekView(entry: entry)
        }
        .configurationDisplayName("Dinner")
        .description("Tonight's dinner and the rest of your week.")
        .supportedFamilies([.systemSmall, .systemMedium, .systemLarge, .accessoryRectangular, .accessoryInline])
        .contentMarginsDisabled()
    }
}

private enum Palette {
    static let accent = Color("AccentColor")
}

struct DinnerWeekView: View {
    let entry: DinnerWeekEntry
    @Environment(\.widgetFamily) private var family

    var body: some View {
        switch family {
        case .systemSmall: SmallDinnerView(entry: entry)
        case .systemMedium: MediumDinnerView(entry: entry)
        case .systemLarge: LargeDinnerView(entry: entry)
        case .accessoryRectangular: RectangularDinnerView(entry: entry)
        case .accessoryInline: InlineDinnerView(entry: entry)
        default: SmallDinnerView(entry: entry)
        }
    }
}

// MARK: - Small: tonight, the photo is the widget

private struct SmallDinnerView: View {
    let entry: DinnerWeekEntry

    var body: some View {
        Group {
            if entry.snapshot == nil {
                SignedOutView()
            } else if let tonight = entry.tonight {
                TonightCard(meal: tonight, photo: entry.photos[tonight.recipeID], nameLines: 3)
                    .widgetURL(DinnerWidgetLink.recipe(id: tonight.recipeID, name: tonight.name).url)
            } else {
                NothingTonightView(next: entry.upcoming.first, weekIsEmpty: entry.snapshot?.hasWeek == false)
            }
        }
        .containerBackground(for: .widget) { Color(.systemBackground) }
    }
}

/// Tonight's photo edge to edge, with the name over a dark fade at the bottom.
private struct TonightCard: View {
    let meal: DinnerWidgetSnapshot.Meal
    let photo: UIImage?
    var nameLines = 2
    var nameFont: Font = .headline

    var body: some View {
        ZStack(alignment: .bottomLeading) {
            PhotoFill(photo: photo)
            LinearGradient(
                colors: [.black.opacity(0), .black.opacity(0.72)], startPoint: .center, endPoint: .bottom)
            VStack(alignment: .leading, spacing: 3) {
                Text("Tonight")
                    .font(.caption2.weight(.bold))
                    .padding(.horizontal, 7)
                    .padding(.vertical, 2)
                    .background(Capsule().fill(Palette.accent))
                Text(meal.name)
                    .font(nameFont)
                    .lineLimit(nameLines)
                    .minimumScaleFactor(0.85)
                if let minutes = meal.minutes {
                    Label("\(minutes) min", systemImage: "clock")
                        .font(.caption2.weight(.medium))
                        .opacity(0.85)
                }
            }
            .foregroundStyle(.white)
            .padding(12)
        }
    }
}

private struct PhotoFill: View {
    let photo: UIImage?

    var body: some View {
        GeometryReader { geometry in
            if let photo {
                Image(uiImage: photo)
                    .resizable()
                    .scaledToFill()
                    .frame(width: geometry.size.width, height: geometry.size.height)
                    .clipped()
            } else {
                ZStack(alignment: .topTrailing) {
                    Palette.accent.opacity(0.85)
                    Image(systemName: "fork.knife")
                        .font(.system(size: 22, weight: .semibold))
                        .foregroundStyle(.white.opacity(0.4))
                        .padding(12)
                }
            }
        }
    }
}

/// Nothing planned tonight: say what's next, or offer to plan the week.
private struct NothingTonightView: View {
    let next: (date: String, meal: DinnerWidgetSnapshot.Meal)?
    let weekIsEmpty: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Image(systemName: weekIsEmpty ? "sparkles" : "moon.stars")
                .font(.title3.weight(.semibold))
                .foregroundStyle(Palette.accent)
            Spacer(minLength: 0)
            Text(weekIsEmpty ? "Plan this week" : "Nothing planned tonight")
                .font(.headline)
                .lineLimit(2)
            if weekIsEmpty {
                Text("Autopilot picks your dinners.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            } else if let next {
                Text("Next: \(DayText.short(next.date)), \(next.meal.name)")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
        .padding(14)
        .widgetURL((weekIsEmpty ? DinnerWidgetLink.autopilot : .menu).url)
    }
}

private struct SignedOutView: View {
    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Image(systemName: "fork.knife.circle.fill")
                .font(.title2)
                .foregroundStyle(Palette.accent)
            Spacer(minLength: 0)
            Text("Open DinnerOS to see your dinners")
                .font(.subheadline.weight(.semibold))
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
        .padding(14)
    }
}

// MARK: - The week rail: seven days, filled where there's dinner

private struct WeekRail: View {
    let snapshot: DinnerWidgetSnapshot
    let today: Date

    var body: some View {
        let key = snapshot.dateKey(today)
        HStack(spacing: 0) {
            ForEach(snapshot.days, id: \.date) { day in
                let planned = !day.meals.isEmpty
                let isToday = day.date == key
                let isPast = day.date < key
                Text(DayText.letter(day.date))
                    .font(.system(size: 11, weight: .heavy, design: .rounded))
                    .foregroundStyle(planned ? Color.white : Color.secondary)
                    .frame(width: 22, height: 22)
                    .background(Circle().fill(planned ? Palette.accent : Color.secondary.opacity(0.14)))
                    .overlay(Circle().strokeBorder(isToday ? Color.primary : .clear, lineWidth: 1.5).padding(-3))
                    .opacity(isPast ? 0.4 : 1)
                    .frame(maxWidth: .infinity)
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(
            Text("\(snapshot.days.filter { !$0.meals.isEmpty }.count) of 7 days planned this week"))
    }
}

/// One upcoming dinner: the day in the app's rounded numerals, then the meal.
private struct UpcomingRow: View {
    let date: String
    let meal: DinnerWidgetSnapshot.Meal
    var photo: UIImage?
    /// The medium widget's narrow column: smaller type, two lines.
    var compact = false

    var body: some View {
        Link(destination: DinnerWidgetLink.recipe(id: meal.recipeID, name: meal.name).url) {
            HStack(spacing: 10) {
                Text(DayText.short(date))
                    .font(.system(.caption, design: .rounded, weight: .heavy))
                    .foregroundStyle(Palette.accent)
                    .frame(width: 30, alignment: .leading)
                if let photo {
                    Image(uiImage: photo)
                        .resizable()
                        .scaledToFill()
                        .frame(width: 26, height: 26)
                        .clipShape(RoundedRectangle(cornerRadius: 6, style: .continuous))
                }
                Text(meal.name)
                    .font(compact ? .caption.weight(.medium) : .subheadline.weight(.medium))
                    .lineLimit(compact ? 2 : 1)
                Spacer(minLength: 0)
            }
        }
    }
}

// MARK: - Medium: tonight beside the week

private struct MediumDinnerView: View {
    let entry: DinnerWeekEntry

    var body: some View {
        Group {
            if let snapshot = entry.snapshot {
                HStack(spacing: 0) {
                    Group {
                        if let tonight = entry.tonight {
                            Link(destination: DinnerWidgetLink.recipe(id: tonight.recipeID, name: tonight.name).url) {
                                TonightCard(
                                    meal: tonight, photo: entry.photos[tonight.recipeID], nameLines: 3,
                                    nameFont: .subheadline.bold())
                            }
                        } else {
                            NothingTonightView(next: nil, weekIsEmpty: !snapshot.hasWeek)
                                .background(Palette.accent.opacity(0.1))
                        }
                    }
                    .frame(width: 138)
                    .frame(maxHeight: .infinity)
                    VStack(alignment: .leading, spacing: 7) {
                        WeekRail(snapshot: snapshot, today: entry.date)
                        let rows = Array(entry.upcoming.prefix(3))
                        if rows.isEmpty {
                            Spacer(minLength: 0)
                            Text(snapshot.hasWeek ? "That's the week." : "Nothing planned yet.")
                                .font(.subheadline)
                                .foregroundStyle(.secondary)
                            Spacer(minLength: 0)
                        } else {
                            ForEach(rows, id: \.date) { row in
                                UpcomingRow(date: row.date, meal: row.meal, compact: true)
                            }
                            Spacer(minLength: 0)
                        }
                    }
                    .padding(12)
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
                }
            } else {
                SignedOutView()
            }
        }
        .containerBackground(for: .widget) { Color(.systemBackground) }
    }
}

// MARK: - Large: tonight, the week, next week, and two shortcuts

private struct LargeDinnerView: View {
    let entry: DinnerWeekEntry

    var body: some View {
        Group {
            if let snapshot = entry.snapshot {
                VStack(alignment: .leading, spacing: 0) {
                    Group {
                        if let tonight = entry.tonight {
                            Link(destination: DinnerWidgetLink.recipe(id: tonight.recipeID, name: tonight.name).url) {
                                TonightCard(
                                    meal: tonight, photo: entry.photos[tonight.recipeID], nameFont: .title3.bold())
                            }
                        } else {
                            NothingTonightView(next: entry.upcoming.first, weekIsEmpty: !snapshot.hasWeek)
                                .background(Palette.accent.opacity(0.1))
                        }
                    }
                    .frame(height: 124)
                    .clipped()

                    VStack(alignment: .leading, spacing: 7) {
                        WeekRail(snapshot: snapshot, today: entry.date)
                            .padding(.bottom, 2)
                        ForEach(Array(entry.upcoming.prefix(3)), id: \.date) { row in
                            UpcomingRow(date: row.date, meal: row.meal, photo: entry.photos[row.meal.recipeID])
                        }
                        nextWeek(snapshot)
                        Spacer(minLength: 0)
                        HStack(spacing: 8) {
                            ActionLink(title: "Plan with Autopilot", symbol: "sparkles", link: .autopilot, filled: true)
                            ActionLink(title: "Grocery List", symbol: "cart", link: .shop, filled: false)
                        }
                    }
                    .padding(.horizontal, 14)
                    .padding(.top, 10)
                    .padding(.bottom, 12)
                }
            } else {
                SignedOutView()
            }
        }
        .containerBackground(for: .widget) { Color(.systemBackground) }
    }

    @ViewBuilder
    private func nextWeek(_ snapshot: DinnerWidgetSnapshot) -> some View {
        Divider().padding(.vertical, 2)
        HStack(alignment: .firstTextBaseline) {
            Text("Next week")
                .font(.caption.weight(.semibold))
                .foregroundStyle(.secondary)
            Spacer()
            if !snapshot.nextWeek.isEmpty {
                Text(snapshot.nextWeek.count == 1 ? "1 meal" : "\(snapshot.nextWeek.count) meals")
                    .font(.system(.caption, design: .rounded, weight: .heavy))
                    .foregroundStyle(Palette.accent)
            }
        }
        if snapshot.nextWeek.isEmpty {
            Text("Not planned yet.")
                .font(.subheadline)
                .foregroundStyle(.secondary)
        } else {
            Text(snapshot.nextWeek.prefix(3).map(\.name).joined(separator: ", "))
                .font(.subheadline)
                .lineLimit(1)
        }
    }
}

private struct ActionLink: View {
    let title: LocalizedStringKey
    let symbol: String
    let link: DinnerWidgetLink
    let filled: Bool

    var body: some View {
        Link(destination: link.url) {
            Label(title, systemImage: symbol)
                .font(.caption.weight(.semibold))
                .lineLimit(1)
                .minimumScaleFactor(0.8)
                .frame(maxWidth: .infinity)
                .padding(.vertical, 8)
                .foregroundStyle(filled ? Color.white : Palette.accent)
                .background(
                    Capsule().fill(filled ? AnyShapeStyle(Palette.accent) : AnyShapeStyle(Palette.accent.opacity(0.14)))
                )
        }
    }
}

// MARK: - Lock screen

private struct RectangularDinnerView: View {
    let entry: DinnerWeekEntry

    var body: some View {
        VStack(alignment: .leading, spacing: 1) {
            Label("Tonight", systemImage: "fork.knife")
                .font(.caption2.weight(.semibold))
                .widgetAccentable()
            if let tonight = entry.tonight {
                Text(tonight.name)
                    .font(.headline)
                    .lineLimit(2)
                if let minutes = tonight.minutes {
                    Text("\(minutes) min")
                        .font(.caption2)
                }
            } else {
                Text(entry.snapshot == nil ? "Open DinnerOS" : "Nothing planned")
                    .font(.headline)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .widgetURL(
            entry.tonight.map { DinnerWidgetLink.recipe(id: $0.recipeID, name: $0.name).url }
                ?? DinnerWidgetLink.menu.url
        )
        .containerBackground(for: .widget) { Color.clear }
    }
}

private struct InlineDinnerView: View {
    let entry: DinnerWeekEntry

    var body: some View {
        Group {
            if let tonight = entry.tonight {
                Label(tonight.name, systemImage: "fork.knife")
            } else {
                Label("No dinner planned", systemImage: "fork.knife")
            }
        }
        .containerBackground(for: .widget) { Color.clear }
    }
}

// MARK: - Dates

private enum DayText {
    private static func date(_ key: String) -> Date? {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.dateFormat = "yyyy-MM-dd"
        return formatter.date(from: key)
    }

    /// "Wed".
    static func short(_ key: String) -> String {
        guard let date = date(key) else { return "" }
        return date.formatted(.dateTime.weekday(.abbreviated))
    }

    /// "W".
    static func letter(_ key: String) -> String {
        guard let date = date(key) else { return "" }
        return date.formatted(.dateTime.weekday(.narrow))
    }
}

// MARK: - Preview data

extension DinnerWidgetSnapshot {
    /// A believable week for the widget gallery and previews.
    static var preview: DinnerWidgetSnapshot {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = .current
        let today = calendar.startOfDay(for: .now)
        let weekday = calendar.component(.weekday, from: today)
        let start = calendar.date(byAdding: .day, value: -(weekday - 1), to: today) ?? today
        let names = [
            nil, "Sweet & Spicy Hoisin Pork Tostadas", nil, "Cheesy Black Bean Tacos",
            "Rigatoni with Beef & Zucchini Ragù", nil, "Thai-Inspired Pad See Ew",
        ]
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.dateFormat = "yyyy-MM-dd"
        var days: [Day] = []
        for (offset, name) in names.enumerated() {
            let date = calendar.date(byAdding: .day, value: offset, to: start) ?? start
            let meals = name.map {
                [Meal(recipeID: "p\(offset)", name: $0, imageURL: nil, minutes: 25, isAddon: false)]
            }
            days.append(Day(date: formatter.string(from: date), meals: meals ?? []))
        }
        // Tonight always has dinner in the gallery.
        let todayKey = formatter.string(from: today)
        if let i = days.firstIndex(where: { $0.date == todayKey }), days[i].meals.isEmpty {
            days[i] = Day(
                date: todayKey,
                meals: [
                    Meal(
                        recipeID: "tonight", name: "Rigatoni with Beef & Zucchini Ragù", imageURL: nil, minutes: 25,
                        isAddon: false)
                ])
        }
        return DinnerWidgetSnapshot(
            updatedAt: .now, days: days, unscheduled: [],
            nextWeek: [
                Meal(recipeID: "n1", name: "Ancho BBQ Burgers", imageURL: nil, minutes: 45, isAddon: false),
                Meal(recipeID: "n2", name: "Honey Garlic Chicken", imageURL: nil, minutes: 30, isAddon: false),
            ],
            timeZone: TimeZone.current.identifier)
    }
}

#Preview("Small", as: .systemSmall) {
    DinnerWeekWidget()
} timeline: {
    DinnerWeekEntry(date: .now, snapshot: .preview, photos: [:])
}

#Preview("Medium", as: .systemMedium) {
    DinnerWeekWidget()
} timeline: {
    DinnerWeekEntry(date: .now, snapshot: .preview, photos: [:])
}

#Preview("Large", as: .systemLarge) {
    DinnerWeekWidget()
} timeline: {
    DinnerWeekEntry(date: .now, snapshot: .preview, photos: [:])
}
