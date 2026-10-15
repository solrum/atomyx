import Flutter
import UIKit

@main
@objc class AppDelegate: FlutterAppDelegate {
  private static let routeArgument = "--route"

  override func application(
    _ application: UIApplication,
    didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?
  ) -> Bool {
    GeneratedPluginRegistrant.register(with: self)
    // A "--route <path>" launch argument selects the first screen, so a
    // launcher can start the app on any route without the deep link prompt.
    if let route = AppDelegate.launchRoute(),
       let controller = window?.rootViewController as? FlutterViewController {
      controller.setInitialRoute(route)
    }
    return super.application(application, didFinishLaunchingWithOptions: launchOptions)
  }

  private static func launchRoute() -> String? {
    let arguments = ProcessInfo.processInfo.arguments
    guard let index = arguments.firstIndex(of: routeArgument), index + 1 < arguments.count else {
      return nil
    }
    return arguments[index + 1]
  }
}
