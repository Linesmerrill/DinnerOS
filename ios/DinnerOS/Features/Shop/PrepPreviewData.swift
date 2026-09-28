import Foundation
import SwiftUI
import UIKit

/// Synthetic prep cards for SwiftUI previews: the Tuesday tacos card in its several shapes.
/// Not DEBUG-only because `#Preview` bodies are type-checked in Release builds too. Photo URLs
/// are made up and never fetched: previews draw them with `PreviewPhotoData`.
enum PrepPreviewData {
    /// 24 oz of ground turkey, 10 oz for Tuesday's tacos: keep 10 oz out, one 10 oz bag, 4 oz over.
    static var oneBag: PrepCard? { decode(oneBagJSON) }
    /// 64 oz for two 10 oz dinners: keep 20 oz out, four 10 oz bags, 4 oz over.
    static var severalBags: PrepCard? { decode(severalBagsJSON) }
    /// 14 oz for a 10 oz dinner: nothing whole to freeze, 4 oz over.
    static var leftoverOnly: PrepCard? { decode(leftoverOnlyJSON) }
    /// A pack whose ingredient has no photo: the category glyph stands in.
    static var noPhoto: PrepCard? { decode(noPhotoJSON) }

    static let oneBagJSON = card(
        id: "l1", name: "Ground Turkey", photo: true, bought: ("24", "24 oz"), needed: ("10", "10 oz"),
        suggested: 1, most: 1, leftovers: ["4 oz"], meals: [("Citrus Turkey Tacos", "tue")])

    static let severalBagsJSON = card(
        id: "l2", name: "Ground Pork", photo: true, bought: ("64", "4 lb"), needed: ("20", "20 oz"),
        suggested: 4, most: 4, leftovers: ["34 oz", "24 oz", "14 oz", "4 oz"],
        meals: [("Citrus Pork Tacos", "tue"), ("Pork Fried Rice", "thu")], portionSize: ("10", "10 oz"))

    static let leftoverOnlyJSON = card(
        id: "l3", name: "Ground Beef", photo: true, bought: ("14", "14 oz"), needed: ("10", "10 oz"),
        suggested: 0, most: 0, leftovers: [], meals: [("Beef Chili", "wed")], leftoverOnly: "4 oz")

    static let noPhotoJSON = card(
        id: "l4", name: "Chicken Thighs", photo: false, bought: ("48", "3 lb"), needed: ("12", "12 oz"),
        suggested: 3, most: 3, leftovers: ["24 oz", "12 oz", ""], meals: [("Honey Garlic Chicken", "mon")],
        portionSize: ("12", "12 oz"))

    /// A session holding `cards` (their JSON), all still to do.
    static func session(_ cards: String...) -> PrepSession? {
        let json = """
            {"week":"2026-W38","state":"ready","headline":"\(cards.count) things to put away.",
             "pending":\(cards.count),"done":0,"skipped":0,"updatedAt":null,
             "cards":[\(cards.joined(separator: ","))]}
            """
        return try? JSONCoding.makeDecoder().decode(PrepSession.self, from: Data(json.utf8))
    }

    /// The loader the previews use: every photo is drawn locally, nothing is fetched.
    static let imageLoader = ImageLoader(data: PreviewPhotoData())

    // MARK: - Building

    private static func decode(_ json: String) -> PrepCard? {
        try? JSONCoding.makeDecoder().decode(PrepCard.self, from: Data(json.utf8))
    }

    private static func card(
        id: String, name: String, photo: Bool, bought: (String, String), needed: (String, String),
        suggested: Int, most: Int, leftovers: [String], meals: [(String, String)],
        portionSize: (String, String)? = nil, leftoverOnly: String? = nil
    ) -> String {
        let size = portionSize ?? needed
        let thaw = #"{"hours":3,"measured":true,"portionOunces":10,"summary":"about 3 hours"}"#
        let options = (0..<most).map { i in
            """
            {"portions":\(i + 1),"size":"\(size.0)","sizeValue":\(size.0),"sizeText":"\(size.1)",
             "leftover":"0","leftoverValue":0,"leftoverText":"\(leftovers[i])","thaw":\(thaw)}
            """
        }
        let leftover = leftoverOnly ?? (suggested > 0 ? leftovers[suggested - 1] : "")
        let mealsJSON = meals.enumerated().map { i, meal in
            """
            {"recipeId":"recipe-\(id)-\(i)","recipeName":"\(meal.0)","day":"\(meal.1)","date":null,"past":false}
            """
        }
        let image = photo ? "https://images.example.com/ingredients/\(id).png" : ""
        return """
            {"id":"handoff-1:\(id)","kind":"bulk_pack","handoffId":"handoff-1","lineId":"\(id)",
             "status":"pending","ingredientKey":"ingredient-\(id)","ingredientId":"ingredient-\(id)",
             "name":"\(name)","imageUrl":"\(image)","category":"meat-seafood",
             "productName":"Sample \(name)","unit":"oz",
             "bought":"\(bought.0)","boughtValue":\(bought.0),"boughtText":"\(bought.1)",
             "needed":"\(needed.0)","neededValue":\(needed.0),"neededText":"\(needed.1)",
             "surplus":"0","surplusValue":0,"surplusPercent":50,"surplusText":"",
             "freezable":true,"frozen":false,"instruction":"","reminder":"thaw",
             "reminderText":"We'll remind you the morning a planned meal needs it — one bag takes about 3 hours in the fridge.",
             "meals":[\(mealsJSON.joined(separator: ","))],
             "portions":{"unit":"oz","reserved":"\(needed.0)","reservedValue":\(needed.0),
                         "reservedText":"\(needed.1)","surplus":"0","surplusValue":0,"meals":\(meals.count),
                         "typicalMeal":"\(size.0)","typicalMealValue":\(size.0),"typicalMealText":"\(size.1)",
                         "basis":"meal","portions":\(suggested),"portionSize":"\(size.0)",
                         "portionSizeValue":\(size.0),"portionSizeText":"\(size.1)",
                         "leftover":"0","leftoverValue":0,"leftoverText":"\(leftover)",
                         "thaw":\(thaw),"options":[\(options.joined(separator: ","))]},
             "frozenItemId":null,"frozenPortions":0,"answeredBy":null,"answeredAt":null,"suggestions":[]}
            """
    }
}

/// Draws a stand-in ingredient photo — a pink, ground-meat-ish tile — so previews show what a
/// real photo looks like next to the glyph fallback, without a network.
nonisolated struct PreviewPhotoData: ImageDataLoading {
    func data(for url: URL) async throws -> Data {
        let size = CGSize(width: 240, height: 240)
        let image = UIGraphicsImageRenderer(size: size).image { context in
            UIColor(red: 0.96, green: 0.93, blue: 0.90, alpha: 1).setFill()
            context.fill(CGRect(origin: .zero, size: size))
            let pinks = [
                UIColor(red: 0.86, green: 0.52, blue: 0.52, alpha: 1),
                UIColor(red: 0.78, green: 0.42, blue: 0.44, alpha: 1),
                UIColor(red: 0.92, green: 0.64, blue: 0.62, alpha: 1),
            ]
            for i in 0..<60 {
                let x = 40 + CGFloat((i * 37) % 160)
                let y = 40 + CGFloat((i * 53) % 160)
                let r = 14 + CGFloat(i % 5) * 3
                pinks[i % pinks.count].setFill()
                context.cgContext.fillEllipse(in: CGRect(x: x - r / 2, y: y - r / 2, width: r, height: r))
            }
        }
        guard let data = image.pngData() else { throw ImageLoadError.invalidResponse }
        return data
    }
}
