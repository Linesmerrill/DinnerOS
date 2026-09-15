import SwiftUI

/// A batch the week needs made: what it makes, which recipes need it, and "Made It".
struct GroceryBatchRow: View {
    let batch: GroceryBatch
    let canMake: Bool
    let isWorking: Bool
    let madeIt: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline, spacing: 12) {
                Image(systemName: "house")
                    .font(.title3)
                    .foregroundStyle(.tint)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 2) {
                    Text(batch.specialtyName)
                        .font(.headline)
                    Text(batch.text)
                        .font(.subheadline)
                    if let recipes = SpecialtyFormat.neededFor(batch.recipes) {
                        Text(recipes)
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                    let detail = SpecialtyFormat.batchDetail(batch)
                    if !detail.isEmpty {
                        Text(detail)
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                }
                .accessibilityElement(children: .combine)
            }
            if canMake {
                HStack(spacing: 8) {
                    Spacer(minLength: 0)
                    if isWorking {
                        ProgressView()
                    }
                    Button("Made It", systemImage: "checkmark.seal", action: madeIt)
                        .buttonStyle(.borderedProminent)
                        // In a list row the symbol otherwise takes the tint and vanishes on the
                        // tinted fill.
                        .foregroundStyle(.white)
                        .controlSize(.small)
                        .disabled(isWorking)
                        .accessibilityLabel("Made \(batch.specialtyName)")
                        .accessibilityHint("Records the batch in the pantry.")
                }
            }
        }
        .padding(.vertical, 2)
    }
}

/// A house-made batch the pantry already covers, shown compactly.
struct GroceryMadeBatchRow: View {
    let batch: GroceryBatch

    var body: some View {
        Label {
            VStack(alignment: .leading, spacing: 2) {
                Text(batch.specialtyName)
                Group {
                    if let remaining = batch.remaining {
                        Text("Already made, about \(remaining.text) left")
                    } else {
                        Text("Already made")
                    }
                }
                .font(.footnote)
                .foregroundStyle(.secondary)
            }
        } icon: {
            Image(systemName: "checkmark.seal.fill")
                .foregroundStyle(.tint)
        }
        .accessibilityElement(children: .combine)
    }
}

/// "2 specialty ingredients need a choice" with a link to the setup screen.
struct SpecialtyChoiceNotice: View {
    let count: Int
    let setUp: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Label(
                count == 1
                    ? String(localized: "1 specialty ingredient needs a choice")
                    : String(localized: "\(count) specialty ingredients need a choice"),
                systemImage: "sparkles"
            )
            .font(.subheadline.weight(.semibold))
            Text("Choose a store alternative or a house-made batch so the list shows what to buy.")
                .font(.footnote)
                .foregroundStyle(.secondary)
            Button("Set Up Specialty Ingredients", action: setUp)
                .buttonStyle(.bordered)
                .controlSize(.small)
        }
    }
}

/// A quick choice for one grocery line: its suggested options, Keep as Is, or every option.
///
/// Choosing closes the picker at once and saves in the background; the line shows progress and
/// the list reloads, like "Add to pantry?" never blocking a shopper.
struct SpecialtyQuickPicker: View {
    let specialty: GrocerySpecialty
    let model: GroceryListModel

    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            List {
                if !specialty.suggestedOptions.isEmpty {
                    Section {
                        ForEach(specialty.suggestedOptions) { option in
                            Button {
                                choose(option.id)
                            } label: {
                                SuggestedOptionRow(option: option)
                            }
                            .accessibilityHint("Chooses this option and updates the grocery list.")
                        }
                    } header: {
                        Text("Suggested Options")
                    } footer: {
                        Text(
                            "A store alternative lists regular ingredients instead. A house-made batch lists what's needed to make a jar you keep in the pantry."
                        )
                    }
                }
                Section {
                    Button("Keep as Is", systemImage: "text.badge.checkmark") {
                        choose(SpecialtyChoice.asIsOptionID)
                    }
                } footer: {
                    Text("Keeps \(specialty.name) on the list by its own name and stops asking.")
                }
                Section {
                    NavigationLink {
                        SpecialtyDetailView(specialtyID: specialty.id, onChoose: { dismiss() })
                    } label: {
                        Label("See All Options", systemImage: "list.bullet")
                    }
                } footer: {
                    Text("Compare ingredients and steps, or customize an option.")
                }
            }
            .navigationTitle(specialty.name)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
            }
        }
        .presentationDetents([.medium, .large])
    }

    private func choose(_ optionID: String) {
        let model = model
        let specialty = specialty
        dismiss()
        Task { await model.choose(optionID: optionID, for: specialty) }
    }
}

private struct SuggestedOptionRow: View {
    let option: GrocerySpecialtyOption

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: option.type.systemImage)
                .foregroundStyle(.tint)
                .frame(minWidth: 28)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(option.name)
                Group {
                    if option.isDefault {
                        Text("\(option.type.title) · Suggested")
                    } else {
                        Text(option.type.title)
                    }
                }
                .font(.subheadline)
                .foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
        }
        // Inside a list Button, hierarchical styles resolve against the tint.
        .foregroundStyle(Color.primary)
        .contentShape(.rect)
    }
}
