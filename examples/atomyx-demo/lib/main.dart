import 'package:flutter/material.dart';
import 'screens/login_screen.dart';
import 'screens/dashboard_screen.dart';
import 'screens/otp_screen.dart';
import 'screens/settings_screen.dart';
import 'screens/product_list_screen.dart';
import 'screens/gestures_screen.dart';

void main() {
  runApp(const AtomyxDemoApp());
}

class AtomyxDemoApp extends StatelessWidget {
  const AtomyxDemoApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Atomyx Demo',
      debugShowCheckedModeBanner: false,
      theme: ThemeData(
        colorSchemeSeed: Colors.blue,
        useMaterial3: true,
      ),
      initialRoute: '/login',
      routes: {
        '/login': (_) => const LoginScreen(),
        '/otp': (_) => const OtpScreen(),
        '/dashboard': (_) => const DashboardScreen(),
        '/settings': (_) => const SettingsScreen(),
        '/products': (_) => const ProductListScreen(),
        '/gestures': (_) => const GesturesScreen(),
      },
    );
  }
}
