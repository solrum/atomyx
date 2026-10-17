import Flutter
import UIKit

class SceneDelegate: FlutterSceneDelegate {
  private static let routeArgument = "--route"
  private static let routeDefaultsKey = "route"

  override func scene(
    _ scene: UIScene,
    willConnectTo session: UISceneSession,
    options connectionOptions: UIScene.ConnectionOptions
  ) {
    // The storyboard window already holds the FlutterViewController here,
    // and the engine has not run yet, so the initial route still applies.
    // A "--route <path>" or "-route <path>" launch argument selects the
    // first screen, so a launcher can start the app on any route without
    // the deep link prompt.
    if let route = SceneDelegate.launchRoute(),
       let controller = window?.rootViewController as? FlutterViewController {
      controller.setInitialRoute(route)
    }
    super.scene(scene, willConnectTo: session, options: connectionOptions)
  }

  private static func launchRoute() -> String? {
    let arguments = ProcessInfo.processInfo.arguments
    guard let index = arguments.firstIndex(of: routeArgument), index + 1 < arguments.count else {
      return UserDefaults.standard.string(forKey: routeDefaultsKey)
    }
    return arguments[index + 1]
  }
}
