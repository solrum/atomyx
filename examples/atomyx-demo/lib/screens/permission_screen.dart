import 'package:flutter/material.dart';
import 'package:permission_handler/permission_handler.dart';

/// Requests the location permission and shows its current status.
class PermissionScreen extends StatefulWidget {
  const PermissionScreen({super.key});

  @override
  State<PermissionScreen> createState() => _PermissionScreenState();
}

class _PermissionScreenState extends State<PermissionScreen> {
  String _status = 'unknown';

  @override
  void initState() {
    super.initState();
    _refresh();
  }

  Future<void> _refresh() async {
    final PermissionStatus status = await Permission.locationWhenInUse.status;
    if (!mounted) return;
    setState(() => _status = status.name);
  }

  Future<void> _request() async {
    final PermissionStatus status =
        await Permission.locationWhenInUse.request();
    if (!mounted) return;
    setState(() => _status = status.name);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Permission')),
      body: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Semantics(
              identifier: 'permission-status',
              label: 'Location status: $_status',
              child: ExcludeSemantics(
                child: Text(
                  'Location status: $_status',
                  textAlign: TextAlign.center,
                  style: const TextStyle(fontSize: 20),
                ),
              ),
            ),
            const SizedBox(height: 24),
            Semantics(
              identifier: 'permission-request-button',
              child: FilledButton(
                onPressed: _request,
                child: const Text('Request location'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
