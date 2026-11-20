import SwiftUI

/// "Customize your meal": swap the protein or double it. Choosing an option saves to the
/// planned meal at once; before the meal is added, the choice is kept and applied with it.
struct RecipeCustomizeSection: View {
    let groups: [CustomizationGroup]
    @Binding var selections: [String: String]
    let choose: (CustomizationGroup, CustomizationChoice) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: DS.Space.l) {
            VStack(alignment: .leading, spacing: DS.Space.hairline) {
                Label("Customize your meal", systemImage: "arrow.triangle.swap")
                    .font(.title3.weight(.semibold))
                    .accessibilityAddTraits(.isHeader)
                Text("Swap the protein or double it.")
                    .font(.subheadline)
                    .foregroundStyle(Color.secondary)
            }
            ForEach(groups) { group in
                VStack(spacing: 0) {
                    ForEach(Array(group.choices.enumerated()), id: \.element.id) { index, choice in
                        Button {
                            choose(group, choice)
                        } label: {
                            row(group: group, choice: choice)
                        }
                        .buttonStyle(.plain)
                        if index < group.choices.count - 1 {
                            Divider()
                                .padding(.leading, 60)
                        }
                    }
                }
                .padding(.vertical, DS.Space.xxs)
                .background(DS.Colors.surface, in: .rect(cornerRadius: DS.Radius.large))
                .accessibilityElement(children: .contain)
                .accessibilityLabel("Choices for \(group.ingredientName)")
            }
        }
    }

    private func row(group: CustomizationGroup, choice: CustomizationChoice) -> some View {
        let isSelected = selections[group.ingredientKey] == choice.id
        return HStack(spacing: DS.Space.m) {
            Image(systemName: isSelected ? "largecircle.fill.circle" : "circle")
                .font(.title3)
                .foregroundStyle(isSelected ? AnyShapeStyle(.tint) : AnyShapeStyle(Color.secondary))
            IngredientPhoto(url: choice.imageURL ?? group.imageURL, size: 40)
            VStack(alignment: .leading, spacing: DS.Space.hairline) {
                Text(choice.label)
                    .font(.headline)
                    .foregroundStyle(Color.primary)
                HStack(spacing: DS.Space.xs) {
                    if !choice.amountText.isEmpty {
                        Text(choice.amountText)
                            .font(.subheadline)
                            .foregroundStyle(Color.secondary)
                    }
                    if let badge = choice.badge {
                        MenuChip(text: badge, isProminent: true)
                    }
                }
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, DS.Space.m)
        .padding(.vertical, DS.Space.sm)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(isSelected ? [.isButton, .isSelected] : .isButton)
    }
}

#Preview("Customize") {
    ScrollView {
        RecipeCustomizeSection(
            groups: MenuPreviewData.customizations, selections: .constant(["i-pork": "beef"])
        ) { _, _ in }
        .padding(DS.Space.l)
    }
}
