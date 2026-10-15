import 'package:flutter/material.dart';
import 'routes.dart';

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
      onGenerateRoute: generateAppRoute,
    );
  }
}
