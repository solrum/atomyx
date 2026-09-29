import 'package:flutter/material.dart';

/// Test fixture for Atomyx's mid-gesture `capture` primitive.
///
/// A long-press opens a context menu; dragging while still holding
/// highlights an item; lifting selects it. The whole interaction must
/// stay one continuous pointer sequence, because the menu auto-
/// dismisses the instant the finger lifts, so it can only ever be
/// screenshotted while the gesture is still held down.
class ContextMenuScreen extends StatefulWidget {
  const ContextMenuScreen({super.key});

  @override
  State<ContextMenuScreen> createState() => _ContextMenuScreenState();
}

enum _MenuItemId { copy, share, delete }

class _ContextMenuScreenState extends State<ContextMenuScreen> {
  static const double _menuItemHeight = 56;
  static const double _menuWidth = 220;
  static const double _menuGapBelowTarget = 8;

  static const List<_MenuItemId> _menuOrder = [
    _MenuItemId.copy,
    _MenuItemId.share,
    _MenuItemId.delete,
  ];

  static const Map<_MenuItemId, String> _itemLabels = {
    _MenuItemId.copy: 'Copy',
    _MenuItemId.share: 'Share',
    _MenuItemId.delete: 'Delete',
  };

  static const Map<_MenuItemId, IconData> _itemIcons = {
    _MenuItemId.copy: Icons.copy,
    _MenuItemId.share: Icons.share,
    _MenuItemId.delete: Icons.delete,
  };

  // The Stack, not the screen, is the coordinate frame the menu is
  // positioned in, because the target lives inside a scrolling list
  // while the menu overlay must float above it at a fixed screen spot.
  final GlobalKey _stackKey = GlobalKey();
  final GlobalKey _targetKey = GlobalKey();

  bool _menuOpen = false;
  Offset _menuTopLeft = Offset.zero;
  _MenuItemId? _highlighted;
  _MenuItemId? _lastSelected;

  String get _stateLabel {
    if (_menuOpen) return 'menu open';
    final last =
        _lastSelected == null ? 'none' : _itemLabels[_lastSelected]!;
    return 'menu closed, last selected: $last';
  }

  void _openMenu() {
    final targetBox =
        _targetKey.currentContext!.findRenderObject() as RenderBox;
    final stackBox =
        _stackKey.currentContext!.findRenderObject() as RenderBox;
    final targetTopLeft = targetBox.localToGlobal(Offset.zero);
    // Anchored directly below the target at a fixed gap, so every
    // menu item lands at a screen coordinate a scripted swipe can
    // predict from the target's own bounds.
    final anchorGlobal = Offset(
      targetTopLeft.dx,
      targetTopLeft.dy + targetBox.size.height + _menuGapBelowTarget,
    );
    setState(() {
      _menuOpen = true;
      _highlighted = null;
      _menuTopLeft = stackBox.globalToLocal(anchorGlobal);
    });
  }

  _MenuItemId? _itemAtGlobalPosition(Offset globalPosition) {
    final stackBox =
        _stackKey.currentContext!.findRenderObject() as RenderBox;
    final local = stackBox.globalToLocal(globalPosition);
    final withinX = local.dx >= _menuTopLeft.dx &&
        local.dx <= _menuTopLeft.dx + _menuWidth;
    final relativeY = local.dy - _menuTopLeft.dy;
    final withinY =
        relativeY >= 0 && relativeY < _menuItemHeight * _menuOrder.length;
    if (!withinX || !withinY) return null;
    return _menuOrder[(relativeY / _menuItemHeight).floor()];
  }

  void _handleLongPressStart(LongPressStartDetails details) {
    _openMenu();
  }

  void _handleLongPressMoveUpdate(LongPressMoveUpdateDetails details) {
    final hit = _itemAtGlobalPosition(details.globalPosition);
    if (hit != _highlighted) {
      setState(() => _highlighted = hit);
    }
  }

  void _handleLongPressEnd(LongPressEndDetails details) {
    final hit = _itemAtGlobalPosition(details.globalPosition);
    setState(() {
      // Lifting off any item selects it; lifting off empty space
      // closes the menu without disturbing the previous selection.
      _lastSelected = hit ?? _lastSelected;
      _menuOpen = false;
      _highlighted = null;
    });
  }

  void _handleLongPressCancel() {
    setState(() {
      _menuOpen = false;
      _highlighted = null;
    });
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Context menu')),
      body: Stack(
        key: _stackKey,
        children: [
          ListView(
            padding: const EdgeInsets.all(16),
            children: [
              _sectionHeader(),
              const SizedBox(height: 12),
              _targetCard(),
            ],
          ),
          if (_menuOpen) _menuOverlay(),
        ],
      ),
    );
  }

  Widget _sectionHeader() {
    const accent = Color(0xFFAD1457); // pink
    return Semantics(
      identifier: 'contextmenu-state',
      label: _stateLabel,
      child: Row(
        children: [
          Container(
            width: 36,
            height: 36,
            decoration: BoxDecoration(
              color: accent.withValues(alpha: 0.15),
              borderRadius: BorderRadius.circular(8),
            ),
            child: const Icon(Icons.menu_open, color: accent),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Text(
                  'Context menu',
                  style: TextStyle(fontWeight: FontWeight.bold, fontSize: 16),
                ),
                const SizedBox(height: 2),
                Text(
                  _stateLabel,
                  style: TextStyle(fontSize: 12, color: Colors.grey.shade700),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _targetCard() {
    const accent = Color(0xFFAD1457); // pink
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const Text(
              'Long-press, keep holding, and drag onto an item, '
              'then lift to select it.',
              style: TextStyle(fontSize: 13),
            ),
            const SizedBox(height: 12),
            Semantics(
              identifier: 'contextmenu-target',
              child: GestureDetector(
                key: _targetKey,
                onLongPressStart: _handleLongPressStart,
                onLongPressMoveUpdate: _handleLongPressMoveUpdate,
                onLongPressEnd: _handleLongPressEnd,
                onLongPressCancel: _handleLongPressCancel,
                child: Container(
                  height: 96,
                  decoration: BoxDecoration(
                    color: _menuOpen
                        ? accent.withValues(alpha: 0.2)
                        : accent.withValues(alpha: 0.08),
                    borderRadius: BorderRadius.circular(12),
                    border: Border.all(color: accent, width: 2),
                  ),
                  alignment: Alignment.center,
                  child: Column(
                    mainAxisAlignment: MainAxisAlignment.center,
                    children: [
                      Icon(Icons.article, color: accent, size: 28),
                      const SizedBox(height: 4),
                      Text(
                        'Hold me',
                        style: TextStyle(
                          color: accent,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _menuOverlay() {
    const accent = Color(0xFFAD1457); // pink
    return Positioned(
      left: _menuTopLeft.dx,
      top: _menuTopLeft.dy,
      width: _menuWidth,
      child: Semantics(
        identifier: 'contextmenu-menu',
        child: Material(
          elevation: 8,
          borderRadius: BorderRadius.circular(8),
          color: Colors.white,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              for (final item in _menuOrder) _menuItem(item, accent),
            ],
          ),
        ),
      ),
    );
  }

  Widget _menuItem(_MenuItemId item, Color accent) {
    final isHighlighted = _highlighted == item;
    return Semantics(
      identifier: 'contextmenu-item-${item.name}',
      label: _itemLabels[item],
      child: Container(
        height: _menuItemHeight,
        color: isHighlighted ? accent.withValues(alpha: 0.2) : Colors.white,
        padding: const EdgeInsets.symmetric(horizontal: 16),
        child: Row(
          children: [
            Icon(
              _itemIcons[item],
              color: isHighlighted ? accent : Colors.grey.shade700,
            ),
            const SizedBox(width: 16),
            Text(
              _itemLabels[item]!,
              style: TextStyle(
                fontSize: 16,
                fontWeight: isHighlighted ? FontWeight.bold : FontWeight.normal,
                color: isHighlighted ? accent : Colors.black87,
              ),
            ),
          ],
        ),
      ),
    );
  }
}
