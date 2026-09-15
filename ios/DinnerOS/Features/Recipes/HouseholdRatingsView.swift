import SwiftUI

/// Opens every member's rating of a recipe.
struct HouseholdRatingsRoute: Hashable {
    let recipeID: String
    let recipeName: String
}

/// Every household member's rating of one recipe, with the average.
struct HouseholdRatingsView: View {
    let route: HouseholdRatingsRoute

    @Environment(RecipeLibrary.self) private var library
    @Environment(AuthSession.self) private var session

    @State private var response: RatingListResponse?
    @State private var loadError: String?

    var body: some View {
        content
            .navigationTitle("Household Ratings")
            .navigationBarTitleDisplayMode(.inline)
            .task { await load() }
    }

    @ViewBuilder
    private var content: some View {
        if let response {
            HouseholdRatingsList(
                recipeName: route.recipeName, response: response, currentUserID: session.currentUser?.id,
                refreshError: loadError
            )
            .refreshable { await load() }
        } else if let loadError {
            ContentUnavailableView {
                Label("Couldn't Load Ratings", systemImage: "exclamationmark.triangle")
            } description: {
                Text(loadError)
            } actions: {
                Button("Try Again") {
                    Task { await load() }
                }
                .buttonStyle(.borderedProminent)
            }
        } else {
            ProgressView("Loading ratings…")
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    private func load() async {
        do {
            response = try await library.ratings(recipeID: route.recipeID)
            loadError = nil
        } catch is CancellationError {
            return
        } catch {
            loadError = HouseholdStore.message(for: error)
        }
    }
}

struct HouseholdRatingsList: View {
    let recipeName: String
    let response: RatingListResponse
    let currentUserID: String?
    var refreshError: String?

    var body: some View {
        List {
            if let refreshError {
                FormErrorLabel(message: refreshError)
            }
            Section {
                VStack(alignment: .leading, spacing: 4) {
                    Text(recipeName)
                        .font(.headline)
                    Text(RatingFormat.summary(response.householdRating) ?? String(localized: "No ratings yet"))
                        .foregroundStyle(.secondary)
                        .accessibilityLabel(RatingFormat.accessibilitySummary(response.householdRating))
                }
            }
            if response.items.isEmpty {
                ContentUnavailableView(
                    "No Ratings Yet", systemImage: "star",
                    description: Text("Nobody in this household has rated this recipe.")
                )
                .listRowBackground(Color.clear)
            } else {
                Section("Members") {
                    ForEach(response.items) { item in
                        MemberRatingRow(item: item, isCurrentUser: item.rating.userID == currentUserID)
                    }
                }
            }
        }
    }
}

private struct MemberRatingRow: View {
    let item: MemberRating
    let isCurrentUser: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline) {
                Text(isCurrentUser ? String(localized: "\(item.name) (You)") : item.name)
                    .font(.headline)
                Spacer(minLength: 8)
                StarsLabel(score: item.rating.score)
            }
            if !item.rating.tags.isEmpty {
                FlowLayout(spacing: 6) {
                    ForEach(item.rating.tags) { tag in
                        TagChip(title: tag.title)
                            .font(.caption)
                    }
                }
                .accessibilityElement(children: .combine)
                .accessibilityLabel("Tags: \(item.rating.tags.map(\.title).formatted(.list(type: .and)))")
            }
            if !item.rating.comment.isEmpty {
                Text(item.rating.comment)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Text(item.rating.updatedAt.formatted(.relative(presentation: .named)))
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .padding(.vertical, 4)
        .accessibilityElement(children: .combine)
    }
}

#Preview {
    NavigationStack {
        HouseholdRatingsList(
            recipeName: RecipePreviewData.recipe.name, response: RecipePreviewData.ratings,
            currentUserID: HouseholdPreviewData.user.id
        )
        .navigationTitle("Household Ratings")
    }
}
