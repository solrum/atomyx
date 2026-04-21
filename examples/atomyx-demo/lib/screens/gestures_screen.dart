import 'package:flutter/material.dart';

/// Test fixture for pointer gestures dispatched by Atomyx.
///
/// Each card demonstrates one gesture pattern with a distinct
/// accent color, icon, and interactive shape so the pattern is
/// recognizable from a screenshot. The visible state string is
/// what the smoke script asserts against; stable accessibility
/// identifiers on every interactive widget keep selectors readable.
class GesturesScreen extends StatefulWidget {
  const GesturesScreen({super.key});

  @override
  State<GesturesScreen> createState() => _GesturesScreenState();
}

class _GesturesScreenState extends State<GesturesScreen> {
  int _tapCount = 0;
  int _longPressCount = 0;
  bool _longPressDown = false;
  Offset _dragOffset = Offset.zero;
  String _dragStatus = 'idle';
  final List<String> _reorderItems = ['Alpha', 'Beta', 'Gamma'];
  double _pinchScale = 1.0;
  int _pinchEvents = 0;
  double _pressurePeak = 0.0;
  int _pressureEvents = 0;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Gestures')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          _tapCard(),
          const SizedBox(height: 12),
          _longPressCard(),
          const SizedBox(height: 12),
          _dragCard(),
          const SizedBox(height: 12),
          _reorderCard(),
          const SizedBox(height: 12),
          _pinchCard(),
          const SizedBox(height: 12),
          _pressureCard(),
        ],
      ),
    );
  }

  Widget _sectionHeader({
    required IconData icon,
    required Color color,
    required String title,
    required String state,
  }) {
    return Row(
      children: [
        Container(
          width: 36,
          height: 36,
          decoration: BoxDecoration(
            color: color.withValues(alpha: 0.15),
            borderRadius: BorderRadius.circular(8),
          ),
          child: Icon(icon, color: color),
        ),
        const SizedBox(width: 12),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(title,
                  style: const TextStyle(
                      fontWeight: FontWeight.bold, fontSize: 16)),
              const SizedBox(height: 2),
              Text(state,
                  style: TextStyle(
                      fontSize: 12, color: Colors.grey.shade700)),
            ],
          ),
        ),
      ],
    );
  }

  // ── Tap ────────────────────────────────────────────────────────
  Widget _tapCard() {
    const accent = Color(0xFFFF6F00); // orange
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Semantics(
              identifier: 'gesture-tap-count',
              label: 'Tap count: $_tapCount',
              child: _sectionHeader(
                icon: Icons.touch_app,
                color: accent,
                title: 'Tap',
                state: 'Count: $_tapCount',
              ),
            ),
            const SizedBox(height: 12),
            Semantics(
              identifier: 'gesture-tap-target',
              child: SizedBox(
                height: 64,
                child: FilledButton.icon(
                  style: FilledButton.styleFrom(
                    backgroundColor: accent,
                    shape: RoundedRectangleBorder(
                      borderRadius: BorderRadius.circular(8),
                    ),
                  ),
                  onPressed: () => setState(() => _tapCount += 1),
                  icon: const Icon(Icons.radio_button_checked),
                  label: const Text(
                    'Tap me',
                    style: TextStyle(fontSize: 16),
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  // ── Long-press ─────────────────────────────────────────────────
  Widget _longPressCard() {
    const accent = Color(0xFF6A1B9A); // purple
    final ringColor = _longPressDown ? accent : accent.withValues(alpha: 0.3);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Semantics(
              identifier: 'gesture-longpress-count',
              label: 'Long-press count: $_longPressCount',
              child: _sectionHeader(
                icon: Icons.timer,
                color: accent,
                title: 'Long-press',
                state: 'Count: $_longPressCount  •  hold ≥500ms',
              ),
            ),
            const SizedBox(height: 12),
            Semantics(
              identifier: 'gesture-longpress-target',
              child: GestureDetector(
                onTapDown: (_) => setState(() => _longPressDown = true),
                onTapUp: (_) => setState(() => _longPressDown = false),
                onTapCancel: () => setState(() => _longPressDown = false),
                onLongPress: () => setState(() {
                  _longPressCount += 1;
                  _longPressDown = false;
                }),
                child: AnimatedContainer(
                  duration: const Duration(milliseconds: 250),
                  height: 96,
                  decoration: BoxDecoration(
                    color: _longPressDown
                        ? accent.withValues(alpha: 0.25)
                        : accent.withValues(alpha: 0.08),
                    borderRadius: BorderRadius.circular(48),
                    border: Border.all(color: ringColor, width: 3),
                  ),
                  child: Center(
                    child: Column(
                      mainAxisAlignment: MainAxisAlignment.center,
                      children: [
                        Icon(Icons.fingerprint, color: accent, size: 32),
                        const SizedBox(height: 4),
                        Text(
                          _longPressDown ? 'Hold…' : 'Press & hold',
                          style: TextStyle(
                              color: accent, fontWeight: FontWeight.w600),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  // ── Drag ───────────────────────────────────────────────────────
  Widget _dragCard() {
    const accent = Color(0xFF1565C0); // blue
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Semantics(
              identifier: 'gesture-drag-status',
              label:
                  'Drag status: $_dragStatus, x=${_dragOffset.dx.toStringAsFixed(0)}, y=${_dragOffset.dy.toStringAsFixed(0)}',
              child: _sectionHeader(
                icon: Icons.open_with,
                color: accent,
                title: 'Drag',
                state:
                    '$_dragStatus  •  dx=${_dragOffset.dx.toStringAsFixed(0)}, dy=${_dragOffset.dy.toStringAsFixed(0)}',
              ),
            ),
            const SizedBox(height: 12),
            Semantics(
              identifier: 'gesture-drag-target',
              child: GestureDetector(
                onPanStart: (_) => setState(() => _dragStatus = 'dragging'),
                onPanUpdate: (d) => setState(() => _dragOffset += d.delta),
                onPanEnd: (_) => setState(() => _dragStatus = 'released'),
                child: Container(
                  height: 140,
                  decoration: BoxDecoration(
                    color: accent.withValues(alpha: 0.06),
                    borderRadius: BorderRadius.circular(12),
                    border: Border.all(
                      color: _dragStatus == 'dragging'
                          ? accent
                          : accent.withValues(alpha: 0.3),
                      width: 2,
                    ),
                  ),
                  child: ClipRRect(
                    borderRadius: BorderRadius.circular(10),
                    child: Stack(
                      children: [
                        Positioned.fill(
                          child: CustomPaint(painter: _CrosshairPainter(accent)),
                        ),
                        Center(
                          child: Container(
                            width: 56,
                            height: 56,
                            decoration: BoxDecoration(
                              color: accent,
                              shape: BoxShape.circle,
                              boxShadow: [
                                BoxShadow(
                                  color: accent.withValues(alpha: 0.4),
                                  blurRadius: 12,
                                  spreadRadius: 2,
                                ),
                              ],
                            ),
                            alignment: Alignment.center,
                            child: const Icon(Icons.drag_indicator,
                                color: Colors.white, size: 32),
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  // ── Reorder ────────────────────────────────────────────────────
  Widget _reorderCard() {
    const accent = Color(0xFF2E7D32); // green
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Semantics(
              identifier: 'gesture-reorder-state',
              label: 'Order: ${_reorderItems.join(", ")}',
              child: _sectionHeader(
                icon: Icons.swap_vert,
                color: accent,
                title: 'Press-and-drag (reorder)',
                state: _reorderItems.join(' → '),
              ),
            ),
            const SizedBox(height: 12),
            SizedBox(
              height: 200,
              child: Semantics(
                identifier: 'gesture-reorder-target',
                child: ReorderableListView(
                  children: [
                    for (var i = 0; i < _reorderItems.length; i++)
                      Container(
                        key: ValueKey(_reorderItems[i]),
                        margin: const EdgeInsets.symmetric(vertical: 2),
                        decoration: BoxDecoration(
                          color: accent.withValues(alpha: 0.08),
                          borderRadius: BorderRadius.circular(8),
                          border: Border.all(
                              color: accent.withValues(alpha: 0.3)),
                        ),
                        child: ListTile(
                          leading: CircleAvatar(
                            backgroundColor: accent,
                            radius: 14,
                            child: Text('${i + 1}',
                                style: const TextStyle(
                                    color: Colors.white, fontSize: 12)),
                          ),
                          title: Text(_reorderItems[i],
                              style:
                                  const TextStyle(fontWeight: FontWeight.w600)),
                          trailing: const Icon(Icons.drag_handle),
                        ),
                      ),
                  ],
                  onReorder: (oldIdx, newIdx) {
                    setState(() {
                      var idx = newIdx;
                      if (idx > oldIdx) idx -= 1;
                      final moved = _reorderItems.removeAt(oldIdx);
                      _reorderItems.insert(idx, moved);
                    });
                  },
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  // Flick fixture intentionally absent: nested-Scrollable gesture
  // arbitration between the outer page ListView and an inner
  // ListView caused synthesized swipes intended for the inner
  // list to be intermittently captured by the outer page,
  // over-scrolling the page instead of advancing inner items.
  // Drag semantics are covered by the Drag card; multi-pointer
  // by the Pinch card.

  // ── Pinch (multi-pointer) ──────────────────────────────────────
  Widget _pinchCard() {
    const accent = Color(0xFF00838F); // teal
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Semantics(
              identifier: 'gesture-pinch-state',
              label:
                  'Pinch scale: ${_pinchScale.toStringAsFixed(2)}, events: $_pinchEvents',
              child: _sectionHeader(
                icon: Icons.zoom_out_map,
                color: accent,
                title: 'Pinch (multi-pointer)',
                state:
                    'scale=${_pinchScale.toStringAsFixed(2)}  •  events=$_pinchEvents',
              ),
            ),
            const SizedBox(height: 12),
            Semantics(
              identifier: 'gesture-pinch-target',
              child: GestureDetector(
                onScaleStart: (_) => setState(() => _pinchEvents += 1),
                onScaleUpdate: (d) =>
                    setState(() => _pinchScale = d.scale.clamp(0.5, 4.0)),
                child: Container(
                  height: 160,
                  decoration: BoxDecoration(
                    color: accent.withValues(alpha: 0.06),
                    borderRadius: BorderRadius.circular(12),
                    border: Border.all(
                        color: accent.withValues(alpha: 0.3), width: 2),
                  ),
                  alignment: Alignment.center,
                  child: Transform.scale(
                    scale: _pinchScale,
                    child: Container(
                      width: 80,
                      height: 80,
                      decoration: BoxDecoration(
                        color: accent,
                        shape: BoxShape.circle,
                      ),
                      child: const Icon(Icons.fingerprint,
                          color: Colors.white, size: 40),
                    ),
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  // ── Pressure (3D Touch / Force Touch) ─────────────────────────
  Widget _pressureCard() {
    const accent = Color(0xFFE65100); // deep orange
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Semantics(
              identifier: 'gesture-pressure-state',
              label:
                  'Pressure peak: ${_pressurePeak.toStringAsFixed(2)}, events: $_pressureEvents',
              child: _sectionHeader(
                icon: Icons.touch_app,
                color: accent,
                title: 'Pressure (3D / Force Touch)',
                state:
                    'peak=${_pressurePeak.toStringAsFixed(2)}  •  events=$_pressureEvents',
              ),
            ),
            const SizedBox(height: 12),
            Semantics(
              identifier: 'gesture-pressure-target',
              child: Listener(
                onPointerDown: _capturePressure,
                onPointerMove: _capturePressure,
                child: Container(
                  height: 120,
                  alignment: Alignment.center,
                  decoration: BoxDecoration(
                    color: Color.lerp(
                      accent.withValues(alpha: 0.06),
                      accent.withValues(alpha: 0.5),
                      _pressurePeak.clamp(0.0, 1.0),
                    ),
                    borderRadius: BorderRadius.circular(12),
                    border: Border.all(color: accent, width: 2),
                  ),
                  child: Text(
                    _pressureEvents == 0
                        ? 'Press here (any device)'
                        : 'Last peak: ${_pressurePeak.toStringAsFixed(2)}',
                    style: TextStyle(
                        color: accent, fontWeight: FontWeight.w600),
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  void _capturePressure(PointerEvent event) {
    setState(() {
      _pressureEvents += 1;
      if (event.pressure > _pressurePeak) {
        _pressurePeak = event.pressure;
      }
    });
  }
}

/// Faint crosshair grid behind the drag puck. Visible directional
/// reference lets a screenshot reader confirm the puck position
/// without needing a ruler.
class _CrosshairPainter extends CustomPainter {
  _CrosshairPainter(this.color);

  final Color color;

  @override
  void paint(Canvas canvas, Size size) {
    final paint = Paint()
      ..color = color.withValues(alpha: 0.15)
      ..strokeWidth = 1;
    canvas.drawLine(
      Offset(0, size.height / 2),
      Offset(size.width, size.height / 2),
      paint,
    );
    canvas.drawLine(
      Offset(size.width / 2, 0),
      Offset(size.width / 2, size.height),
      paint,
    );
    final dashPaint = Paint()
      ..color = color.withValues(alpha: 0.08)
      ..strokeWidth = 1;
    for (var x = 20.0; x < size.width; x += 20) {
      canvas.drawLine(Offset(x, 0), Offset(x, size.height), dashPaint);
    }
    for (var y = 20.0; y < size.height; y += 20) {
      canvas.drawLine(Offset(0, y), Offset(size.width, y), dashPaint);
    }
  }

  @override
  bool shouldRepaint(covariant _CrosshairPainter old) => old.color != color;
}
