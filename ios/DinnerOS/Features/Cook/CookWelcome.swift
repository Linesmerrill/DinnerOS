import SwiftUI

/// The first time someone opens the cooking screen: four pages, each a small drawing of the part
/// of the screen it's about and a line or two on what it does. Reopened from the ? button.
struct CookWelcome: View {
    let done: () -> Void

    @State private var page = 0
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private let pages: [CookWelcomePage] = [
        CookWelcomePage(
            title: "Welcome to cooking with DinnerOS",
            lines: [
                "Your ingredients are on the left, grouped by the step that uses them.",
                "Check them off as they go in.",
                "Press and hold one to flag its product, so you pick a different one next time.",
            ],
            drawing: .ingredients),
        CookWelcomePage(
            title: "Follow the steps",
            lines: [
                "Tap a step to highlight it, so you can find your place again.",
                "Tap a green time to start a timer. Add or take away time before you start it.",
            ],
            drawing: .steps),
        CookWelcomePage(
            title: "Run a few timers at once",
            lines: [
                "Each timer is named for what it's cooking.",
                "When one's done, add a minute or stop it.",
                "Press and hold a timer to move it.",
            ],
            drawing: .timers),
        CookWelcomePage(
            title: "Make it yours",
            lines: [
                "Tap the list button at the top to show ingredients all together.",
                "Tap the stacked squares to switch to another dish this week.",
                "Enjoy!",
            ],
            drawing: .toolbar),
    ]

    var body: some View {
        VStack(spacing: 0) {
            TabView(selection: $page) {
                ForEach(pages.indices, id: \.self) { index in
                    pageView(pages[index])
                        .tag(index)
                }
            }
            .tabViewStyle(.page(indexDisplayMode: .always))
            .indexViewStyle(.page(backgroundDisplayMode: .always))

            HStack {
                if page < pages.count - 1 {
                    Button("Skip", action: done)
                        .foregroundStyle(.secondary)
                }
                Spacer()
                Button {
                    if page < pages.count - 1 {
                        if reduceMotion { page += 1 } else { withAnimation { page += 1 } }
                    } else {
                        done()
                    }
                } label: {
                    Text(page < pages.count - 1 ? "Next" : "Start Cooking")
                        .font(.headline)
                        .padding(.horizontal, 12)
                }
                .buttonStyle(.borderedProminent)
            }
            .padding(20)
        }
        .frame(idealWidth: 560, idealHeight: 640)
    }

    private func pageView(_ page: CookWelcomePage) -> some View {
        ScrollView {
            VStack(spacing: 22) {
                CookWelcomeDrawing(kind: page.drawing)
                    .frame(maxWidth: 420)
                    .frame(height: 230)
                    .padding(.top, 28)
                    .accessibilityHidden(true)
                Text(page.title)
                    .font(.title2.weight(.bold))
                    .multilineTextAlignment(.center)
                VStack(alignment: .leading, spacing: 10) {
                    ForEach(Array(page.lines.enumerated()), id: \.offset) { _, line in
                        Text(line)
                            .font(.body)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
                .frame(maxWidth: 420, alignment: .leading)
            }
            .padding(.horizontal, 24)
            .padding(.bottom, 40)
        }
    }
}

private struct CookWelcomePage {
    let title: LocalizedStringKey
    let lines: [LocalizedStringKey]
    let drawing: CookWelcomeDrawing.Kind
}

/// A small drawing of the real screen, in the app's own colors and shapes.
private struct CookWelcomeDrawing: View {
    enum Kind { case ingredients, steps, timers, toolbar }
    let kind: Kind

    var body: some View {
        ZStack {
            RoundedRectangle(cornerRadius: 22, style: .continuous)
                .fill(Color(.secondarySystemBackground))
            content
                .padding(22)
        }
    }

    @ViewBuilder
    private var content: some View {
        switch kind {
        case .ingredients: ingredients
        case .steps: steps
        case .timers: timers
        case .toolbar: toolbar
        }
    }

    private var ingredients: some View {
        VStack(alignment: .leading, spacing: 10) {
            heading("Step 1")
            row(checked: true, amount: "2", name: "Scallions", prep: "sliced")
            row(checked: false, amount: "1", name: "Lime", prep: "quartered")
            row(checked: false, amount: "8", name: "Tortillas", prep: nil, flagged: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var steps: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Step 3")
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(Color.accentColor)
            (Text("Stir in ") + Text("1 zucchini").bold() + Text(". Cook until tender, ")
                + Text(Image(systemName: "timer")).foregroundStyle(Color.accentColor)
                + Text(" 2–3 minutes").bold().foregroundStyle(Color.accentColor) + Text("."))
                .font(.callout)
                .fixedSize(horizontal: false, vertical: true)
            HStack(spacing: 14) {
                circleButton("minus")
                Text("2:00")
                    .font(.system(size: 34, weight: .heavy, design: .rounded).monospacedDigit())
                circleButton("plus")
            }
            .frame(maxWidth: .infinity)
            .padding(.top, 4)
        }
        .padding(14)
        .background(
            RoundedRectangle(cornerRadius: 14).fill(Color.accentColor.opacity(0.12))
        )
        .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(Color.accentColor, lineWidth: 2))
    }

    private var timers: some View {
        VStack(alignment: .trailing, spacing: 10) {
            timerPill("5:30", name: "Zucchini", total: "6 min", progress: 0.1, tint: .accentColor)
            timerPill("0:42", name: "Pasta", total: "10 min", progress: 0.93, tint: .orange)
            timerPill("Done", name: "Tortillas", total: "4 min", progress: 1, tint: .white, finished: true)
        }
        .frame(maxWidth: .infinity, alignment: .trailing)
    }

    private var toolbar: some View {
        VStack(spacing: 18) {
            HStack(spacing: 22) {
                toolbarIcon("rectangle.on.rectangle", label: "Other dishes")
                toolbarIcon("list.bullet.indent", label: "Ingredient layout")
                toolbarIcon("questionmark.circle", label: "This guide")
            }
            HStack(spacing: 12) {
                miniLayout(byStep: true)
                miniLayout(byStep: false)
            }
        }
    }

    // MARK: Pieces

    private func heading(_ text: String) -> some View {
        Text(text)
            .font(.headline)
            .padding(.bottom, 3)
            .overlay(alignment: .bottom) { Capsule().fill(Color.accentColor).frame(height: 3).offset(y: 2) }
    }

    private func row(checked: Bool, amount: String, name: String, prep: String?, flagged: Bool = false) -> some View {
        HStack(spacing: 12) {
            RoundedRectangle(cornerRadius: 5)
                .strokeBorder(checked ? Color.accentColor : Color.secondary.opacity(0.6), lineWidth: 1.5)
                .background(RoundedRectangle(cornerRadius: 5).fill(checked ? Color.accentColor : .clear))
                .overlay {
                    if checked {
                        Image(systemName: "checkmark").font(.caption.bold()).foregroundStyle(.white)
                    }
                }
                .frame(width: 22, height: 22)
            Text(amount).foregroundStyle(.secondary).frame(width: 22)
            VStack(alignment: .leading, spacing: 0) {
                HStack(spacing: 6) {
                    Text(name).fontWeight(.semibold).strikethrough(checked)
                    if flagged {
                        Image(systemName: "flag.fill").font(.caption).foregroundStyle(.orange)
                    }
                }
                if let prep {
                    Text(prep).font(.caption).italic().foregroundStyle(.secondary)
                }
            }
        }
        .font(.callout)
    }

    private func circleButton(_ symbol: String) -> some View {
        Image(systemName: symbol)
            .font(.headline.bold())
            .frame(width: 38, height: 38)
            .background(Circle().fill(Color.accentColor.opacity(0.16)))
            .foregroundStyle(Color.accentColor)
    }

    private func timerPill(
        _ time: String, name: String, total: String, progress: Double, tint: Color, finished: Bool = false
    ) -> some View {
        HStack(spacing: 10) {
            ZStack {
                Circle().stroke(tint.opacity(0.25), lineWidth: 3.5)
                Circle().trim(from: 0, to: finished ? 1 : 1 - progress)
                    .stroke(tint, style: StrokeStyle(lineWidth: 3.5, lineCap: .round))
                    .rotationEffect(.degrees(-90))
                Image(systemName: finished ? "bell.fill" : "timer").font(.caption.bold()).foregroundStyle(tint)
            }
            .frame(width: 32, height: 32)
            VStack(alignment: .leading, spacing: 0) {
                Text(time)
                    .font(.system(.headline, design: .rounded, weight: .heavy).monospacedDigit())
                    .foregroundStyle(finished ? Color.white : tint == .orange ? Color.orange : Color.primary)
                (Text(name).fontWeight(.semibold) + Text("  \(total) timer"))
                    .font(.caption2)
                    .foregroundStyle(finished ? Color.white.opacity(0.9) : Color.secondary)
            }
        }
        .padding(.leading, 6)
        .padding(.trailing, 14)
        .padding(.vertical, 6)
        .background(
            Capsule().fill(finished ? AnyShapeStyle(Color.accentColor) : AnyShapeStyle(Color(.systemBackground))))
    }

    private func toolbarIcon(_ symbol: String, label: String) -> some View {
        VStack(spacing: 6) {
            Image(systemName: symbol)
                .font(.title3)
                .frame(width: 48, height: 48)
                .background(Circle().fill(Color(.systemBackground)))
                .foregroundStyle(Color.accentColor)
            Text(label).font(.caption2).foregroundStyle(.secondary)
        }
    }

    private func miniLayout(byStep: Bool) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            ForEach(0..<(byStep ? 2 : 1), id: \.self) { group in
                if byStep {
                    Text("Step \(group + 1)").font(.system(size: 8, weight: .bold)).foregroundStyle(Color.accentColor)
                }
                ForEach(0..<(byStep ? 2 : 5), id: \.self) { _ in
                    HStack(spacing: 4) {
                        RoundedRectangle(cornerRadius: 2).stroke(Color.secondary, lineWidth: 1).frame(
                            width: 7, height: 7)
                        Capsule().fill(Color.secondary.opacity(0.45)).frame(width: 48, height: 4)
                    }
                }
            }
        }
        .padding(10)
        .frame(width: 96, height: 84, alignment: .topLeading)
        .background(RoundedRectangle(cornerRadius: 10).fill(Color(.systemBackground)))
    }
}

#Preview {
    CookWelcome {}
}
