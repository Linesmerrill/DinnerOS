import SwiftUI

/// How the cooking screen lists ingredients.
enum CookIngredientLayout: String, CaseIterable, Identifiable {
    /// Grouped under the step that uses them, with how each is prepped.
    case byStep
    /// One list, the way the recipe card has it.
    case all
    var id: String { rawValue }
}

/// Two little drawings of the ingredient list to choose between, because "by step" and "all at
/// once" only make sense when you can see them.
struct CookLayoutPicker: View {
    @Binding var layout: CookIngredientLayout

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Show ingredients")
                .font(.headline)
            HStack(spacing: 12) {
                option(.byStep, title: "By Step") { miniByStep }
                option(.all, title: "All Together") { miniAll }
            }
        }
        .padding(18)
    }

    private func option(
        _ value: CookIngredientLayout, title: LocalizedStringKey, @ViewBuilder drawing: () -> some View
    ) -> some View {
        let selected = layout == value
        return Button {
            withAnimation(.snappy(duration: 0.2)) { layout = value }
        } label: {
            VStack(spacing: 8) {
                drawing()
                    .padding(12)
                    .frame(width: 128, height: 128, alignment: .topLeading)
                    .background(RoundedRectangle(cornerRadius: 14).fill(Color(.secondarySystemBackground)))
                    .overlay(
                        RoundedRectangle(cornerRadius: 14)
                            .strokeBorder(selected ? Color.accentColor : Color.clear, lineWidth: 2.5))
                Label(title, systemImage: selected ? "checkmark.circle.fill" : "circle")
                    .font(.subheadline.weight(selected ? .semibold : .regular))
                    .foregroundStyle(selected ? Color.accentColor : Color.primary)
            }
        }
        .buttonStyle(.plain)
        .accessibilityLabel(title)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    /// Two step headings, each with a couple of rows under it.
    private var miniByStep: some View {
        VStack(alignment: .leading, spacing: 5) {
            ForEach(0..<2, id: \.self) { step in
                Text("Step \(step + 1)")
                    .font(.system(size: 9, weight: .bold))
                    .foregroundStyle(Color.accentColor)
                ForEach(0..<(step == 0 ? 2 : 3), id: \.self) { row in
                    line(width: [52, 40, 60][row])
                }
            }
        }
    }

    /// One long list.
    private var miniAll: some View {
        VStack(alignment: .leading, spacing: 7) {
            ForEach(0..<7, id: \.self) { row in
                line(width: [56, 44, 64, 38, 50, 60, 42][row])
            }
        }
    }

    private func line(width: CGFloat) -> some View {
        HStack(spacing: 5) {
            RoundedRectangle(cornerRadius: 2).stroke(Color.secondary, lineWidth: 1).frame(width: 8, height: 8)
            Capsule().fill(Color.secondary.opacity(0.45)).frame(width: width, height: 5)
        }
    }
}
