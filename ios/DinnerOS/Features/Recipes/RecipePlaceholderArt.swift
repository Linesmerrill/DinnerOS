import SwiftUI

/// What a recipe without a photo shows: a warm gradient with a faint pattern of kitchen shapes,
/// an icon for the kind of dish, and the name where there's room. The colors come from the name,
/// so a recipe always looks the same and neighbours look different.
struct RecipePlaceholderArt: View {
    var title: String?
    /// Whether to write the name. Small thumbnails and the recipe page's own header leave it off.
    var showsTitle = true
    /// Fades the icon and name as they near the top of the screen. Done while drawing, from
    /// where they are, so scrolling never re-renders the view.
    var fadesNearTop = false

    var body: some View {
        let palette = RecipePlaceholderStyle.palette(for: title ?? "")
        GeometryReader { geometry in
            let size = geometry.size
            let compact = min(size.width, size.height) < 110
            ZStack {
                LinearGradient(colors: palette.colors, startPoint: .topLeading, endPoint: .bottomTrailing)
                RecipePlaceholderPattern(symbols: RecipePlaceholderStyle.patternSymbols)
                    .foregroundStyle(.white.opacity(0.10))
                VStack(spacing: compact ? 0 : 8) {
                    Image(systemName: RecipePlaceholderStyle.symbol(for: title ?? ""))
                        .font(.system(size: compact ? size.height * 0.34 : min(size.width, size.height) * 0.2))
                        .fontWeight(.medium)
                        .foregroundStyle(.white.opacity(0.9))
                        .shadow(color: .black.opacity(0.15), radius: 4, y: 2)
                    if showsTitle, !compact, let title, !title.isEmpty {
                        Text(title)
                            .font(size.width > 260 ? .title3.weight(.bold) : .subheadline.weight(.bold))
                            .foregroundStyle(.white)
                            .multilineTextAlignment(.center)
                            .lineLimit(2)
                            .minimumScaleFactor(0.8)
                            .padding(.horizontal, DS.Space.m)
                            .shadow(color: .black.opacity(0.2), radius: 3, y: 1)
                    }
                }
                .visualEffect { content, proxy in
                    content.opacity(fadesNearTop ? RecipePlaceholderStyle.fade(top: proxy.frame(in: .global).minY) : 1)
                }
            }
            .frame(width: size.width, height: size.height)
        }
        .accessibilityHidden(true)
    }
}

/// A faint tiled pattern of small kitchen shapes, drawn once with Canvas.
private struct RecipePlaceholderPattern: View {
    let symbols: [String]

    var body: some View {
        Canvas { context, size in
            let step: CGFloat = 46
            let resolved = symbols.map { context.resolveSymbol(id: $0) }
            var row = 0
            var y: CGFloat = 12
            while y < size.height + step {
                var x: CGFloat = row.isMultiple(of: 2) ? 10 : 10 + step / 2
                var column = 0
                while x < size.width + step {
                    if let symbol = resolved[(row + column) % resolved.count] {
                        context.draw(symbol, at: CGPoint(x: x, y: y))
                    }
                    x += step
                    column += 1
                }
                y += step
                row += 1
            }
        } symbols: {
            ForEach(symbols, id: \.self) { name in
                Image(systemName: name)
                    .font(.system(size: 15, weight: .semibold))
                    .tag(name)
            }
        }
        .allowsHitTesting(false)
    }
}

enum RecipePlaceholderStyle {
    struct Palette: Equatable {
        let colors: [Color]
    }

    /// Warm kitchen colors: tomato, saffron, olive, plum, cinnamon, citrus.
    static let palettes: [Palette] = [
        Palette(colors: [Color(red: 0.91, green: 0.38, blue: 0.27), Color(red: 0.96, green: 0.62, blue: 0.33)]),
        Palette(colors: [Color(red: 0.95, green: 0.65, blue: 0.20), Color(red: 0.89, green: 0.42, blue: 0.22)]),
        Palette(colors: [Color(red: 0.47, green: 0.58, blue: 0.30), Color(red: 0.73, green: 0.70, blue: 0.33)]),
        Palette(colors: [Color(red: 0.62, green: 0.27, blue: 0.36), Color(red: 0.87, green: 0.45, blue: 0.40)]),
        Palette(colors: [Color(red: 0.62, green: 0.38, blue: 0.24), Color(red: 0.86, green: 0.58, blue: 0.36)]),
        Palette(colors: [Color(red: 0.93, green: 0.52, blue: 0.20), Color(red: 0.97, green: 0.76, blue: 0.30)]),
    ]

    /// Fully shown 200 points below the top of the screen, gone by 100: the buttons sit above that.
    static func fade(top: CGFloat) -> Double {
        min(1, max(0, Double(top - 100) / 100))
    }

    static let patternSymbols = ["fork.knife", "leaf", "flame", "carrot", "cup.and.saucer", "birthday.cake"]

    /// The same name always gets the same colors. Swift's own hash changes every launch, so
    /// this sums the letters instead.
    static func palette(for title: String) -> Palette {
        let sum = title.lowercased().unicodeScalars.reduce(0) { ($0 &* 31 &+ Int($1.value)) & 0xFFFF }
        return palettes[sum % palettes.count]
    }

    /// An icon for the kind of dish, read from words in its name.
    static func symbol(for title: String) -> String {
        let words = Set(
            title.lowercased().split { !$0.isLetter }.map { word -> String in
                let w = String(word)
                return w.hasSuffix("s") && w.count > 3 ? String(w.dropLast()) : w
            })
        for (keys, symbol) in kinds where !words.isDisjoint(with: keys) {
            return symbol
        }
        return "fork.knife"
    }

    private static let kinds: [(Set<String>, String)] = [
        (
            [
                "cookie", "cake", "brownie", "pie", "cupcake", "dessert", "bar", "muffin", "fudge", "tart",
                "cheesecake", "pudding", "cobbler", "crisp",
            ], "birthday.cake"
        ),
        (["salad", "slaw", "veggie", "vegetable", "kale", "spinach", "greens"], "leaf"),
        (["fish", "salmon", "tuna", "shrimp", "cod", "tilapia", "crab", "scallop", "seafood"], "fish"),
        (["chicken", "turkey", "duck", "wing"], "bird"),
        (["steak", "beef", "burger", "brisket", "bbq", "grill", "grilled", "rib", "pork", "kebab"], "flame"),
        (["soup", "chili", "stew", "chowder", "ramen", "pho", "curry", "broth"], "takeoutbag.and.cup.and.straw"),
        (["pancake", "waffle", "egg", "omelet", "breakfast", "toast", "french"], "sun.horizon"),
        (["smoothie", "coffee", "tea", "latte", "shake", "drink", "lemonade"], "cup.and.saucer"),
        (["carrot", "potato", "squash", "roasted"], "carrot"),
        (["bread", "loaf", "roll", "biscuit", "bagel", "pizza"], "oven"),
    ]
}
