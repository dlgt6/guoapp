import 'package:flutter/material.dart';

import 'focus_ring.dart';

class PinyinKeyboard extends StatelessWidget {
  const PinyinKeyboard({
    super.key,
    required this.onKeyPress,
    required this.onBackspace,
    required this.onClear,
    required this.onSearch,
    this.hasInput = false,
  });
  final ValueChanged<String> onKeyPress;
  final VoidCallback onBackspace;
  final VoidCallback onClear;
  final VoidCallback onSearch;
  final bool hasInput;

  static const _keyRows = [
    ['A', 'B', 'C', 'D', 'E', 'F'],
    ['G', 'H', 'I', 'J', 'K', 'L'],
    ['M', 'N', 'O', 'P', 'Q', 'R'],
    ['S', 'T', 'U', 'V', 'W', 'X'],
    ['Y', 'Z', '1', '2', '3', '4'],
    ['5', '6', '7', '8', '9', '0'],
  ];

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    return Column(
      spacing: 10,
      children: [
        for (final row in _keyRows)
          Expanded(
            child: Row(
              spacing: 8,
              children: [
                for (final char in row)
                  Expanded(
                    child: FocusRing(
                      radius: 10,
                      child: Material(
                        color: colors.surfaceContainerHigh,
                        borderRadius: BorderRadius.circular(10),
                        clipBehavior: Clip.antiAlias,
                        child: InkWell(
                          onTap: () => onKeyPress(char),
                          child: Center(
                            child: Text(
                              char,
                              style: const TextStyle(
                                fontSize: 19,
                                fontWeight: FontWeight.bold,
                              ),
                            ),
                          ),
                        ),
                      ),
                    ),
                  ),
              ],
            ),
          ),
        Row(
          spacing: 8,
          children: [
            Expanded(
              child: _actionButton(
                context,
                '退格',
                Icons.backspace_outlined,
                hasInput ? onBackspace : null,
              ),
            ),
            Expanded(
              child: _actionButton(
                context,
                '清空',
                Icons.delete_outline_rounded,
                hasInput ? onClear : null,
              ),
            ),
            Expanded(
              child: _actionButton(
                context,
                '搜索',
                Icons.search_rounded,
                hasInput ? onSearch : null,
              ),
            ),
          ],
        ),
      ],
    );
  }

  Widget _actionButton(
    BuildContext context,
    String label,
    IconData icon,
    VoidCallback? onPressed,
  ) {
    final colors = Theme.of(context).colorScheme;
    return FocusRing(
      radius: 10,
      child: Material(
        color: onPressed != null
            ? colors.surfaceContainerHigh
            : colors.surfaceContainerHigh.withValues(alpha: 0.5),
        borderRadius: BorderRadius.circular(10),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: onPressed,
          child: Padding(
            padding: const EdgeInsets.symmetric(vertical: 10),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(icon, size: 20),
                const SizedBox(height: 2),
                Text(label, style: const TextStyle(fontSize: 13)),
              ],
            ),
          ),
        ),
      ),
    );
  }
}