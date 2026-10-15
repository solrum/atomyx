import 'package:flutter/material.dart';
import 'screens/context_menu_screen.dart';
import 'screens/dashboard_screen.dart';
import 'screens/focus_screen.dart';
import 'screens/gestures_screen.dart';
import 'screens/login_screen.dart';
import 'screens/media_viewer_screen.dart';
import 'screens/otp_screen.dart';
import 'screens/permission_screen.dart';
import 'screens/product_list_screen.dart';
import 'screens/settings_screen.dart';
import 'screens/webview_screen.dart';

const String defaultRoute = '/login';
const String deepLinkScheme = 'atomyxdemo';
const String deepLinkOpenHost = 'open';

final Map<String, WidgetBuilder> appRoutes = {
  '/login': (_) => const LoginScreen(),
  '/otp': (_) => const OtpScreen(),
  '/dashboard': (_) => const DashboardScreen(),
  '/settings': (_) => const SettingsScreen(),
  '/products': (_) => const ProductListScreen(),
  '/gestures': (_) => const GesturesScreen(),
  '/context-menu': (_) => const ContextMenuScreen(),
  '/media': (_) => const MediaViewerScreen(),
  '/focus': (_) => const FocusScreen(),
  '/permission': (_) => const PermissionScreen(),
  '/webview': (_) => const WebviewScreen(),
};

/// Maps a route name or a deep link URL to a registered route path.
///
/// Accepts a plain path (`/settings`), a URL in the form
/// `atomyxdemo://open/settings`, or `atomyxdemo:///settings`. Anything
/// malformed or unregistered resolves to [defaultRoute].
String resolveRoute(String? name) {
  if (name == null || name.isEmpty) return defaultRoute;
  final Uri? uri = Uri.tryParse(name);
  if (uri == null) return defaultRoute;
  String path = uri.path;
  if (uri.scheme == deepLinkScheme &&
      uri.host.isNotEmpty &&
      uri.host != deepLinkOpenHost) {
    path = '/${uri.host}${uri.path}';
  }
  if (path.length > 1 && path.endsWith('/')) {
    path = path.substring(0, path.length - 1);
  }
  return appRoutes.containsKey(path) ? path : defaultRoute;
}

Route<dynamic> generateAppRoute(RouteSettings settings) {
  final String path = resolveRoute(settings.name);
  return MaterialPageRoute<void>(
    settings: RouteSettings(name: path, arguments: settings.arguments),
    builder: appRoutes[path]!,
  );
}
