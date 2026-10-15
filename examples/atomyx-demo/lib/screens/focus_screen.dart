import 'package:flutter/material.dart';

/// A single text field that takes focus as soon as the screen opens and
/// starts with known text, so an erase case has something to erase.
class FocusScreen extends StatefulWidget {
  const FocusScreen({super.key});

  static const String prefilledText = 'Prefilled text';

  @override
  State<FocusScreen> createState() => _FocusScreenState();
}

class _FocusScreenState extends State<FocusScreen> {
  final _controller = TextEditingController(text: FocusScreen.prefilledText);

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Focus')),
      body: Padding(
        padding: const EdgeInsets.all(24),
        child: Semantics(
          identifier: 'focus-input',
          child: TextField(
            controller: _controller,
            autofocus: true,
            decoration: const InputDecoration(
              labelText: 'Focus field',
              border: OutlineInputBorder(),
            ),
          ),
        ),
      ),
    );
  }
}
