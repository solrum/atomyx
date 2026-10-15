import 'package:flutter/material.dart';
import 'package:webview_flutter/webview_flutter.dart';

/// Hosts a bundled local page. The page carries a heading, a text input,
/// and a button with ids `webview-heading`, `webview-input`, and
/// `webview-button`.
class WebviewScreen extends StatefulWidget {
  const WebviewScreen({super.key});

  static const String pageAsset = 'assets/webview/index.html';

  @override
  State<WebviewScreen> createState() => _WebviewScreenState();
}

class _WebviewScreenState extends State<WebviewScreen> {
  late final WebViewController _controller;

  @override
  void initState() {
    super.initState();
    _controller = WebViewController()
      ..setJavaScriptMode(JavaScriptMode.unrestricted)
      ..loadFlutterAsset(WebviewScreen.pageAsset);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Webview')),
      body: WebViewWidget(controller: _controller),
    );
  }
}
