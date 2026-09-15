import Foundation

/// Synthetic Autopilot data for SwiftUI previews, decoded from JSON shaped like the API's.
/// Not DEBUG-only because `#Preview` bodies are type-checked in Release builds too.
enum AutopilotPreviewData {
    static let week = PlanPreviewData.week

    static let profile: AutopilotProfile? = decode(
        #"""
        {"householdId":"household-1","configured":true,
         "taste":{"likes":{"cuisines":["mexican"],"tags":[],"proteins":["chicken","pork"]},
                  "dislikes":{"cuisines":[],"tags":[],"proteins":["lamb"]}},
         "restrictions":{"diets":[],"allergens":["peanuts"],"excludedIngredients":["cilantro"],"excludedCuisines":[],
                         "excludedProteins":[],"excludedTags":[],"noSpicy":false},
         "schedule":{"planDays":["mon","tue","wed","thu","sun"],"weeknights":["mon","tue","wed","thu"],
                     "mealsPerWeek":4,"defaultServings":null,"weeknightMaxMinutes":30},
         "cookTime":{"quickMaxMinutes":20,"mediumMaxMinutes":35,"maxLongPerWeek":1,"minQuickPerWeek":2,
                     "avoidConsecutiveLong":true},
         "novelty":"balanced","equipment":["smoker"],
         "weekdayRules":[{"day":"sun","label":"Smoker night","cuisines":[],"tags":[],"proteins":["chicken","pork"],
                          "methods":["smoker"],"timeBand":"long","frequency":"every_week"}],
         "sections":{"taste":{"updatedBy":"user-ada","updatedAt":"2026-09-14T18:30:00Z"},"restrictions":null,
                     "schedule":null,"cookTime":{"updatedBy":"user-charles","updatedAt":"2026-09-15T08:10:00Z"},
                     "novelty":null,"equipment":null,"weekdayRules":null},
         "effective":{"defaultServings":4},"createdBy":"user-ada","createdAt":"2026-09-14T18:30:00Z",
         "updatedBy":"user-charles","updatedAt":"2026-09-15T08:10:00Z"}
        """#)

    static let vocabulary: AutopilotVocabulary? = decode(
        #"""
        {"cuisines":[{"value":"mexican","label":"Mexican","recipeCount":42},{"value":"italian","label":"Italian","recipeCount":30},
                     {"value":"thai","label":"Thai","recipeCount":0}],
         "tags":[{"value":"comfort food","label":"Comfort Food","recipeCount":12},{"value":"one pot","label":"One Pot","recipeCount":9}],
         "proteins":[{"value":"chicken","label":"Chicken","recipeCount":120},{"value":"beef","label":"Beef","recipeCount":70},
                     {"value":"pork","label":"Pork","recipeCount":64},{"value":"lamb","label":"Lamb","recipeCount":0}],
         "diets":[{"value":"vegetarian","label":"Vegetarian","description":"No meat or fish"},
                  {"value":"gluten-free","label":"Gluten-free"}],
         "allergens":[{"value":"milk","label":"Milk"},{"value":"peanuts","label":"Peanuts"}],
         "equipment":[{"value":"smoker","label":"Smoker","description":"Whole or large cuts of chicken, pork, beef, or turkey"},
                      {"value":"grill","label":"Grill"},{"value":"air-fryer","label":"Air fryer"}],
         "novelty":[{"value":"favorites","label":"Mostly favorites","description":"Stick to meals we know we like"},
                    {"value":"balanced","label":"A mix","description":"Favorites with something new now and then"},
                    {"value":"adventurous","label":"Try new things","description":"Lean toward meals we haven't had"}],
         "timeBands":[{"value":"quick","label":"Quick"},{"value":"medium","label":"Medium"},
                      {"value":"long","label":"Long cook OK","description":"Longer than the medium limit"}],
         "frequencies":[{"value":"every_week","label":"Every week"},{"value":"at_most_once","label":"At most once a week"}],
         "days":[{"value":"mon","label":"Monday"}],"catalogRecipeCount":3,
         "limits":{"maxListValues":30,"maxExcludedIngredients":50,"maxValueLength":40,"maxIngredientLength":60,
                   "maxRuleValues":10,"maxLabelLength":40,"maxNoteLength":500,"minCookMinutes":5,"maxCookMinutes":480,
                   "maxServings":12}}
        """#)

    static let proposal: AutopilotProposal? = decode(
        #"""
        {"id":"proposal-1","householdId":"household-1","week":"2026-W38","startDate":"2026-09-14","endDate":"2026-09-20",
         "status":"proposed","version":2,"attempt":1,"modelVersion":"baseline-2026.1","inputsHash":"preview",
         "requestedMeals":4,"plannedMeals":3,"candidateCount":3,"coldStart":false,
         "slots":[
           {"id":"mon","day":"mon","date":"2026-09-14","recipe":{"id":"recipe-1","name":"Skillet Test Tacos"},"servings":4,
            "cookMinutes":18,"timeBand":"quick","score":1,"signals":{},
            "reasons":[{"code":"weeknight","text":"18 min, easy for a weeknight"},{"code":"rating","text":"Rated 4.5★ by your household"}],
            "swapCount":0},
           {"id":"wed","day":"wed","date":"2026-09-16","recipe":{"id":"recipe-3","name":"Placeholder Pasta Bake"},"servings":4,
            "cookMinutes":30,"timeBand":"medium","score":0.8,"signals":{},"reasons":[{"code":"new","text":"Something new to try"}],
            "swapCount":1},
           {"id":"sun","day":"sun","date":"2026-09-20","recipe":{"id":"recipe-4","name":"Sample Smoked Pork Shoulder"},"servings":4,
            "cookMinutes":240,"timeBand":"long","score":1.2,"signals":{},
            "reasons":[{"code":"rule","text":"Smoker night · Pork · Long cook OK"}],"swapCount":0}
         ],
         "unfilled":[{"day":"thu","date":"2026-09-17","code":"no_quick_candidates","text":"No remaining recipe is ready within 20 minutes on Thursday."}],
         "messages":[{"code":"not_enough_candidates","text":"Only 3 recipes match; planned 3 of 4 nights."}],
         "objective":{"meals":3,"variety":0,"cookTime":0,"rules":0,"novelty":0,"total":3},
         "swapCount":1,"excludedSlotIds":[],"generatedBy":"user-ada","generatedAt":"2026-09-14T19:02:00Z",
         "updatedAt":"2026-09-14T19:03:10Z","decidedBy":null,"decidedAt":null}
        """#)

    static let context: AutopilotWeekContext? = decode(
        #"""
        {"householdId":"household-1","week":"2026-W38","startDate":"2026-09-14","endDate":"2026-09-20","configured":true,
         "skip":false,"busy":true,"mealsPerWeek":null,"maxMinutes":null,"servings":null,
         "days":[{"day":"fri","skip":false,"maxMinutes":null,"servings":6}],"note":"Guests Friday",
         "updatedBy":"user-ada","updatedAt":"2026-09-14T19:00:00Z"}
        """#)

    static func store(session: AuthSession, configured: Bool = true, withProposal: Bool = true) -> AutopilotStore {
        .preview(
            session: session, profile: configured ? profile : nil, vocabulary: vocabulary,
            proposal: withProposal ? proposal : nil, context: context)
    }

    private static func decode<Value: Decodable>(_ json: String) -> Value? {
        try? JSONCoding.makeDecoder().decode(Value.self, from: Data(json.utf8))
    }
}
