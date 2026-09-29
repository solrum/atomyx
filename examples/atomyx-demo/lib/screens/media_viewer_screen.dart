import 'dart:math' as math;

import 'package:flutter/material.dart';

/// Test fixture for Atomyx's two-finger `pinch` and `rotate` gestures.
///
/// A single manipulable media target fills the screen and applies both
/// the scale and the rotation carried by one continuous two-finger
/// gesture, reporting each live value so a script can assert on them.
///
/// The target is deliberately NOT inside a scrollable. A bare
/// `GestureDetector`'s scale recognizer contests the gesture arena with
/// any ancestor `Scrollable`'s drag recognizer, and the scrollable wins
/// often enough that a synthesized two-finger gesture never reaches the
/// target. With no scrollable ancestor the scale recognizer claims the
/// gesture cleanly, so both scale and rotation land every time.
class MediaViewerScreen extends StatefulWidget {
  const MediaViewerScreen({super.key});

  @override
  State<MediaViewerScreen> createState() => _MediaViewerScreenState();
}

class _MediaViewerScreenState extends State<MediaViewerScreen> {
  static const Color _accent = Color(0xFF3949AB); // indigo

  // Committed transform from all completed gestures. The live gesture's
  // scale and rotation multiply onto these at their gesture-start value,
  // so successive gestures accumulate instead of snapping back.
  double _committedScale = 1.0;
  double _committedRotation = 0.0;

  // Live transform shown to the user and reported via the state label.
  double _scale = 1.0;
  double _rotation = 0.0;

  double get _rotationDegrees => _rotation * 180 / math.pi;

  String get _stateLabel =>
      'scale=${_scale.toStringAsFixed(2)}, '
      'rotation=${_rotationDegrees.toStringAsFixed(0)}';

  void _handleScaleStart(ScaleStartDetails details) {
    _committedScale = _scale;
    _committedRotation = _rotation;
  }

  void _handleScaleUpdate(ScaleUpdateDetails details) {
    setState(() {
      _scale = _committedScale * details.scale;
      _rotation = _committedRotation + details.rotation;
    });
  }

  void _reset() {
    setState(() {
      _committedScale = 1.0;
      _committedRotation = 0.0;
      _scale = 1.0;
      _rotation = 0.0;
    });
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Media viewer')),
      body: Column(
        children: [
          _header(),
          Expanded(
            child: Semantics(
              identifier: 'media-target',
              child: GestureDetector(
                onScaleStart: _handleScaleStart,
                onScaleUpdate: _handleScaleUpdate,
                onDoubleTap: _reset,
                child: Container(
                  color: _accent.withValues(alpha: 0.04),
                  alignment: Alignment.center,
                  child: Transform(
                    alignment: Alignment.center,
                    transform: Matrix4.identity()
                      ..scaleByDouble(_scale, _scale, 1, 1)
                      ..rotateZ(_rotation),
                    child: CustomPaint(
                      size: const Size(240, 240),
                      painter: _MediaPainter(_accent),
                    ),
                  ),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _header() {
    return Semantics(
      identifier: 'media-state',
      label: _stateLabel,
      child: Container(
        width: double.infinity,
        padding: const EdgeInsets.all(16),
        color: _accent.withValues(alpha: 0.08),
        child: Row(
          children: [
            Container(
              width: 36,
              height: 36,
              decoration: BoxDecoration(
                color: _accent.withValues(alpha: 0.15),
                borderRadius: BorderRadius.circular(8),
              ),
              child: const Icon(Icons.image, color: _accent),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Text(
                    'Media viewer',
                    style:
                        TextStyle(fontWeight: FontWeight.bold, fontSize: 16),
                  ),
                  const SizedBox(height: 2),
                  Text(
                    'Two-finger pinch and rotate  •  double-tap resets',
                    style:
                        TextStyle(fontSize: 12, color: Colors.grey.shade700),
                  ),
                  const SizedBox(height: 2),
                  Text(
                    _stateLabel,
                    style: const TextStyle(
                      fontSize: 13,
                      fontWeight: FontWeight.w600,
                      color: _accent,
                    ),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Asymmetric "photo" for the media target: a labelled grid with a
/// single arrow pointing up. The arrow makes rotation unmistakable in a
/// screenshot, and the fixed grid division count makes the square size a
/// direct visual gauge of the live scale.
class _MediaPainter extends CustomPainter {
  _MediaPainter(this.color);

  final Color color;
  static const int _divisions = 4;

  @override
  void paint(Canvas canvas, Size size) {
    final framePaint = Paint()
      ..color = color
      ..style = PaintingStyle.stroke
      ..strokeWidth = 3;
    final fillPaint = Paint()..color = color.withValues(alpha: 0.10);
    final rect = Offset.zero & size;
    canvas.drawRect(rect, fillPaint);
    canvas.drawRect(rect, framePaint);

    final gridPaint = Paint()
      ..color = color.withValues(alpha: 0.30)
      ..strokeWidth = 1;
    final stepX = size.width / _divisions;
    final stepY = size.height / _divisions;
    for (var i = 1; i < _divisions; i++) {
      canvas.drawLine(
        Offset(stepX * i, 0),
        Offset(stepX * i, size.height),
        gridPaint,
      );
      canvas.drawLine(
        Offset(0, stepY * i),
        Offset(size.width, stepY * i),
        gridPaint,
      );
    }

    _paintArrow(canvas, size);

    final textPainter = TextPainter(
      textDirection: TextDirection.ltr,
      text: TextSpan(
        text: 'TOP',
        style: TextStyle(
          color: color,
          fontSize: 12,
          fontWeight: FontWeight.bold,
        ),
      ),
    )..layout();
    textPainter.paint(canvas, Offset(size.width / 2 - textPainter.width / 2, 6));
  }

  void _paintArrow(Canvas canvas, Size size) {
    final arrowPaint = Paint()
      ..color = color
      ..style = PaintingStyle.fill;
    final cx = size.width / 2;
    final top = size.height * 0.28;
    final bottom = size.height * 0.72;
    final shaft = Paint()
      ..color = color
      ..strokeWidth = 6
      ..strokeCap = StrokeCap.round;
    canvas.drawLine(Offset(cx, bottom), Offset(cx, top), shaft);
    final head = Path()
      ..moveTo(cx, top - size.height * 0.10)
      ..lineTo(cx - size.width * 0.08, top + size.height * 0.02)
      ..lineTo(cx + size.width * 0.08, top + size.height * 0.02)
      ..close();
    canvas.drawPath(head, arrowPaint);
  }

  @override
  bool shouldRepaint(covariant _MediaPainter old) => old.color != color;
}
