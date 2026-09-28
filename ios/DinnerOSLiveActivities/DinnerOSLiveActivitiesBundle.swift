import SwiftUI
import WidgetKit

/// The widget extension's entry point. It holds Live Activities only; DinnerOS has no home
/// screen widgets.
@main
struct DinnerOSLiveActivitiesBundle: WidgetBundle {
    var body: some Widget {
        MealKitImportLiveActivity()
    }
}
