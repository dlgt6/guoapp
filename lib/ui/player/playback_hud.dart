import 'dart:async';

import 'package:flutter/material.dart';
import 'package:media_kit/media_kit.dart';

String _formatClock(Duration value) {
  final seconds = value.inSeconds.clamp(0, 999999);
  String two(int number) => number.toString().padLeft(2, '0');
  final clock = '${two(seconds ~/ 60 % 60)}:${two(seconds % 60)}';
  return seconds >= 3600 ? '${seconds ~/ 3600}:$clock' : clock;
}

class PlaybackHud extends StatefulWidget {
  const PlaybackHud({
    super.key,
    required this.title,
    required this.player,
    this.television = false,
  });

  final String title;
  final Player player;
  final bool television;

  @override
  State<PlaybackHud> createState() => _PlaybackHudState();
}

class _PlaybackHudState extends State<PlaybackHud> {
  Timer? _timer;
  DateTime _lastSample = DateTime.now();
  Duration _lastBuffer = Duration.zero;
  double _downloadKbps = 0;

  @override
  void initState() {
    super.initState();
    _lastBuffer = widget.player.state.buffer;
    _timer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (!mounted) return;
      final now = DateTime.now();
      final buffer = widget.player.state.buffer;
      final dt = now.difference(_lastSample).inMilliseconds;
      final db = buffer.inMilliseconds - _lastBuffer.inMilliseconds;
      if (dt > 0) {
        final rate = db / dt;
        final kbps = rate > 0 ? rate * 187.5 : 0;
        _downloadKbps = _downloadKbps * 0.6 + kbps * 0.4;
      }
      _lastSample = now;
      _lastBuffer = buffer;
      setState(() {});
    });
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final position = widget.player.state.position;
    final duration = widget.player.state.duration;
    final time = MaterialLocalizations.of(context).formatTimeOfDay(
      TimeOfDay.fromDateTime(DateTime.now()),
    );
    final progress =
        '${_formatClock(position)} / ${_formatClock(duration)}';
    final speed = _downloadKbps > 1
        ? '~${_downloadKbps.round()} KB/s'
        : '--';
    final fontSize = (widget.television ? 14 : 11) * 1.55;
    final style = TextStyle(
      fontSize: fontSize,
      color: Colors.white,
      fontFeatures: const [FontFeature.tabularFigures()],
      shadows: const [
        Shadow(color: Colors.black, blurRadius: 3, offset: Offset(1, 1)),
      ],
    );
    final inset = widget.television ? 16.0 : 10.0;
    return IgnorePointer(
      child: Stack(
        fit: StackFit.expand,
        children: [
          Positioned(
            top: inset,
            left: inset,
            right: inset * 3,
            child: Text(
              widget.title,
              style: style,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
          ),
          Positioned(top: inset, right: inset, child: Text(time, style: style)),
          Positioned(
            bottom: inset,
            left: inset,
            child: Text(progress, style: style),
          ),
          Positioned(
            bottom: inset,
            right: inset,
            child: Text(speed, style: style),
          ),
        ],
      ),
    );
  }
}