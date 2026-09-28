import SwiftUI
import WidgetKit

/// The widget extension's entry point: the recipe-import Live Activity and the Dinner widget.
@main
struct DinnerOSLiveActivitiesBundle: WidgetBundle {
    var body: some Widget {
        MealKitImportLiveActivity()
        DinnerWeekWidget()
    }
}
