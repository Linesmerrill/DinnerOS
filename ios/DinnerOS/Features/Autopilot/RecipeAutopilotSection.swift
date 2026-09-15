import SwiftUI

/// A recipe's Autopilot details on its screen: cook time and band, and whether it's good
/// for each piece of the household's equipment (Automatic, Yes, or No).
struct RecipeAutopilotSection: View {
    let recipeID: String

    @Environment(AutopilotStore.self) private var autopilot
    @Environment(HouseholdStore.self) private var households

    @State private var attributes: AutopilotRecipeAttributes?
    @State private var loadError: String?
    @State private var savingMethod: String?
    @State private var saveError: String?

    private var canEdit: Bool {
        households.access?.can(.planEdit) == true
    }

    /// The household's equipment once Autopilot is set up; every method before that.
    private var shownMethods: [AutopilotMethod] {
        guard let attributes else { return [] }
        guard let profile = autopilot.profile, profile.configured else { return attributes.methods }
        return attributes.methods.filter { profile.equipment.contains($0.method) }
    }

    var body: some View {
        DetailSection("Autopilot") {
            if let attributes {
                HStack(spacing: 8) {
                    Label(AutopilotFormat.cookTime(attributes.cookMinutes), systemImage: "clock")
                    TimeBandBadge(band: attributes.timeBand)
                }
                .foregroundStyle(.secondary)
                .accessibilityElement(children: .combine)
                ForEach(shownMethods) { method in
                    methodRow(method)
                }
                if shownMethods.isEmpty {
                    Text("Add equipment in Autopilot Preferences to mark recipes for it.")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                if let saveError {
                    FormErrorLabel(message: saveError)
                }
            } else if let loadError {
                VStack(alignment: .leading, spacing: 8) {
                    FormErrorLabel(message: loadError)
                    Button("Try Again") {
                        Task { await load() }
                    }
                    .buttonStyle(.bordered)
                }
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .task(id: recipeID) {
            if let householdID = households.current?.household.id {
                await autopilot.activate(householdID: householdID)
            }
            await load()
        }
    }

    private func methodRow(_ method: AutopilotMethod) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text("Good for \(method.label)")
                Spacer()
                if savingMethod == method.method {
                    ProgressView()
                } else if canEdit {
                    Picker(
                        "Good for \(method.label)",
                        selection: Binding(get: { method.setting }, set: { set(method, to: $0) })
                    ) {
                        ForEach(AutopilotMethodSetting.allCases) { setting in
                            Text(AutopilotFormat.methodSettingTitle(setting, heuristicSuits: method.heuristicSuits))
                                .tag(setting)
                        }
                    }
                    .labelsHidden()
                    .pickerStyle(.menu)
                } else {
                    Text(method.suits ? "Yes" : "No")
                        .foregroundStyle(.secondary)
                }
            }
            Text(explanation(method))
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
        .accessibilityElement(children: .contain)
    }

    private func explanation(_ method: AutopilotMethod) -> String {
        let automatic =
            method.heuristicSuits
            ? (method.evidence.map { String(localized: "Automatically yes: matched “\($0)”.") }
                ?? String(localized: "Automatically yes."))
            : String(localized: "Automatically no.")
        return method.setting == .automatic ? automatic : String(localized: "Set by your household. \(automatic)")
    }

    private func load() async {
        do {
            attributes = try await autopilot.attributes(recipeID: recipeID)
            loadError = nil
        } catch is CancellationError {
            return
        } catch {
            loadError = HouseholdStore.message(for: error)
        }
    }

    private func set(_ method: AutopilotMethod, to setting: AutopilotMethodSetting) {
        guard setting != method.setting else { return }
        savingMethod = method.method
        saveError = nil
        Task {
            defer { savingMethod = nil }
            do {
                attributes = try await autopilot.setMethod(method.method, to: setting, recipeID: recipeID)
            } catch is CancellationError {
                return
            } catch {
                saveError = HouseholdStore.message(for: error)
            }
        }
    }
}
