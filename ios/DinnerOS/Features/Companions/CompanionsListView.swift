import SwiftUI

/// Opens the Companions list from the Menu's ⋯ menu.
struct CompanionsRoute: Hashable {}

/// The household's companions: what goes with dinner, with edit and delete. Reached from the
/// Menu's ⋯ menu and from Autopilot's Pairings.
struct CompanionsListView: View {
    @Environment(CompanionsStore.self) private var companions: CompanionsStore?
    @Environment(HouseholdStore.self) private var households

    @State private var editing: Companion?
    @State private var isAdding = false
    @State private var errorMessage: String?

    private var householdID: String? { households.current?.household.id }

    private var canEdit: Bool { households.access?.can(.recipesEdit) == true }

    private var items: [Companion] { companions?.items ?? [] }

    var body: some View {
        List {
            if items.isEmpty {
                if case .failed(let message) = companions?.phase {
                    Section {
                        FormErrorLabel(message: message)
                    }
                } else if companions?.phase == .loaded {
                    ContentUnavailableView {
                        Label("No Companions Yet", systemImage: "takeoutbag.and.cup.and.straw")
                    } description: {
                        Text("Chips with tacos, bread with soup. Add the things that go with dinner.")
                    }
                } else {
                    ProgressView()
                        .frame(maxWidth: .infinity)
                }
            } else {
                Section {
                    ForEach(items) { companion in
                        row(companion)
                    }
                } footer: {
                    Text("Autopilot suggests these with the meals they go with, or adds them to your list for you.")
                }
            }
            if let errorMessage {
                Section {
                    FormErrorLabel(message: errorMessage)
                }
            }
        }
        .navigationTitle("Companions")
        .toolbar {
            if canEdit, householdID != nil {
                ToolbarItem(placement: .primaryAction) {
                    Button("Add a Companion", systemImage: "plus") { isAdding = true }
                }
            }
        }
        .dsSheet(isPresented: $isAdding) {
            if let householdID {
                CompanionEditorView(householdID: householdID, onSaved: { (_: Companion) in })
            }
        }
        .dsSheet(item: $editing) { companion in
            if let householdID {
                CompanionEditorView(householdID: householdID, companion: companion)
            }
        }
        .task(id: householdID) {
            guard let householdID else { return }
            await companions?.load(householdID: householdID)
        }
        .refreshable {
            guard let householdID else { return }
            await companions?.load(householdID: householdID)
        }
    }

    private func row(_ companion: Companion) -> some View {
        Button {
            if canEdit { editing = companion }
        } label: {
            VStack(alignment: .leading, spacing: DS.Space.hairline) {
                Text(companion.name)
                    .foregroundStyle(Color.primary)
                if !companion.summary.isEmpty {
                    Text(companion.summary)
                        .font(.caption)
                        .foregroundStyle(Color.secondary)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("CompanionRow")
        .swipeActions {
            if canEdit {
                Button("Delete", role: .destructive) { delete(companion) }
            }
        }
    }

    private func delete(_ companion: Companion) {
        guard let companions, let householdID else { return }
        Task {
            do {
                try await companions.delete(id: companion.id, householdID: householdID)
                errorMessage = nil
            } catch is CancellationError {
                return
            } catch {
                errorMessage = HouseholdStore.message(for: error)
            }
        }
    }
}

#Preview("Companions") {
    let session = HouseholdPreviewData.session()
    NavigationStack {
        CompanionsListView()
    }
    .environment(
        CompanionsStore.preview(
            session: session,
            items: [
                Companion(
                    id: "1", name: "Chips and salsa", cuisines: ["mexican"],
                    summary: "Goes with Mexican · Suggested"),
                Companion(
                    id: "2", name: "Garlic Bread", linkedRecipe: CompanionRecipe(id: "r1", name: "Garlic Bread"),
                    mealCategories: ["pasta"], summary: "Goes with pasta meals · Suggested"),
            ])
    )
    .environment(HouseholdPreviewData.store(session: session))
}
