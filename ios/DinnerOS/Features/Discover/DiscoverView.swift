import SwiftUI

/// "Try Something Else": recipes from the global catalog the household doesn't
/// have yet, and a search across the whole catalog.
///
/// With the search field empty it shows discovery, which leaves out the
/// household's own recipes; typing searches everything and marks the ones the
/// household already has, because "you already have this" is an answer.
struct DiscoverView: View {
    @Environment(DiscoverStore.self) private var discover
    @Environment(HouseholdStore.self) private var households

    @State private var searchText = ""

    private var canAdd: Bool {
        households.access?.can(.recipesEdit) == true
    }

    var body: some View {
        @Bindable var discover = discover
        List {
            if let message = discover.refreshError {
                DiscoverNotice(message: message)
            }
            switch discover.phase {
            case .idle, .loading:
                ForEach(0..<4, id: \.self) { _ in
                    DiscoverRow(summary: CatalogPreviewData.placeholder, canAdd: false, isAdding: false) {}
                        .redacted(reason: .placeholder)
                }
            case .failed(let message):
                DiscoverErrorRow(message: message) {
                    Task { await discover.retry() }
                }
            case .loaded where discover.items.isEmpty:
                emptyState
            case .loaded:
                ForEach(discover.items) { item in
                    NavigationLink(value: CatalogRecipeRoute(id: item.id, name: item.name)) {
                        DiscoverRow(
                            summary: item, canAdd: canAdd, isAdding: discover.adding.contains(item.id)
                        ) {
                            Task { await discover.add(item) }
                        }
                    }
                }
                if discover.hasMore {
                    loadMoreRow
                }
            }
        }
        .listStyle(.plain)
        .navigationTitle("Try Something Else")
        .navigationBarTitleDisplayMode(.inline)
        .searchable(text: $searchText, prompt: Text("Search every recipe"))
        .refreshable { await discover.refresh() }
        .alert(
            "Couldn't Add That Recipe", isPresented: .constant(discover.addError != nil),
            presenting: discover.addError
        ) { _ in
            Button("OK") { discover.addError = nil }
        } message: { message in
            Text(message)
        }
        .task(id: households.current?.household.id) {
            guard let id = households.current?.household.id else { return }
            await discover.activate(householdID: id)
        }
        .task(id: searchText) {
            // Debounced: a search per keystroke would be a request per keystroke.
            guard searchText != discover.query.search else { return }
            try? await Task.sleep(for: .milliseconds(350))
            guard !Task.isCancelled else { return }
            await discover.setSearch(searchText)
        }
    }

    private var emptyState: some View {
        Group {
            if discover.isSearching {
                ContentUnavailableView.search(text: discover.query.normalizedSearch)
            } else {
                ContentUnavailableView(
                    "Nothing New Right Now", systemImage: "sparkles",
                    description: Text("You already have every recipe we know about. Search to look again."))
            }
        }
        .listRowSeparator(.hidden)
    }

    private var loadMoreRow: some View {
        Group {
            if let message = discover.loadMoreError {
                DiscoverErrorRow(message: message) {
                    Task { await discover.loadMore() }
                }
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity)
                    .task { await discover.loadMore() }
            }
        }
        .listRowSeparator(.hidden)
    }
}

/// One catalog recipe in the list, with its "Add" button.
struct DiscoverRow: View {
    let summary: CatalogSummary
    let canAdd: Bool
    let isAdding: Bool
    let add: () -> Void

    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @ScaledMetric(relativeTo: .body) private var thumbnailWidth = 88.0

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
                Text(Self.details(for: summary))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                if let reason = summary.reasons.first {
                    Label(reason, systemImage: "sparkles")
                        .font(.caption)
                        .foregroundStyle(.tint)
                }
                addControl
            }
        }
        .padding(.vertical, 4)
    }

    @ViewBuilder
    private var addControl: some View {
        if summary.inLibrary {
            Label("In your library", systemImage: "checkmark.circle.fill")
                .font(.caption.weight(.semibold))
                .foregroundStyle(.secondary)
        } else if canAdd {
            Button {
                add()
            } label: {
                if isAdding {
                    ProgressView()
                } else {
                    Label("Add to Library", systemImage: "plus.circle")
                }
            }
            .buttonStyle(.bordered)
            .buttonBorderShape(.capsule)
            .controlSize(.small)
            .disabled(isAdding)
            .accessibilityLabel("Add \(summary.name) to your library")
            // The row is a navigation link; the button must not open it.
            .buttonRepeatBehavior(.disabled)
        }
    }

    /// For example "Add-on · 30 min · Thai".
    static func details(for summary: CatalogSummary) -> String {
        var parts: [String] = []
        if summary.isAddon {
            parts.append(String(localized: "Add-on"))
        }
        if let minutes = summary.displayMinutes {
            parts.append(RecipeFormat.minutes(minutes))
        }
        parts.append(contentsOf: summary.cuisines.prefix(2))
        if parts.isEmpty {
            parts.append(contentsOf: summary.tags.prefix(2))
        }
        return parts.joined(separator: " · ")
    }
}

/// A non-blocking notice above the list, for a refresh that failed.
struct DiscoverNotice: View {
    let message: String

    var body: some View {
        Label(message, systemImage: "exclamationmark.triangle")
            .font(.footnote)
            .foregroundStyle(.secondary)
            .listRowSeparator(.hidden)
    }
}

/// A failure with a way out of it.
struct DiscoverErrorRow: View {
    let message: String
    let retry: () -> Void

    var body: some View {
        VStack(spacing: 8) {
            Text(message)
                .font(.footnote)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
            Button("Try Again", action: retry)
                .buttonStyle(.bordered)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 12)
        .listRowSeparator(.hidden)
    }
}
