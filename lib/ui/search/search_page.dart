import 'dart:async';

import 'package:flutter/material.dart';

import '../../core_bridge.dart';
import '../../drama_feed.dart';
import '../../local_store.dart';
import '../../models.dart';
import '../../source_catalog.dart';
import '../app_scope.dart';
import '../detail/detail_page.dart';
import '../shell/app_shell.dart';
import '../widgets/drama_grid.dart';
import '../widgets/focus_ring.dart';
import '../widgets/pinyin_keyboard.dart';
import '../widgets/search_field.dart';
import '../widgets/status_view.dart';

class SearchPage extends StatefulWidget {
  const SearchPage({
    super.key,
    required this.store,
    required this.source,
    this.query = '',
  });
  final LocalStore store;
  final SourceSite source;
  final String query;

  @override
  State<SearchPage> createState() => SearchPageState();
}

class SearchPageState extends State<SearchPage> {
  late final _controller = TextEditingController(text: widget.query);
  final _focus = FocusNode();
  late AppRepository _repository;
  late SourceCatalog _catalog;
  DramaFeed? _results;
  String _submitted = '';
  List<String> _suggestions = const [];
  Timer? _debounce;
  int _suggestion = 0;
  bool _ready = false;
  bool _searching = false;
  final _suggestionCache = <String, List<String>>{};

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (_ready) return;
    _ready = true;
    _repository = AppScope.of(context).repository;
    _catalog = SourceCatalog(_repository, widget.source);
    if (widget.query.trim().isNotEmpty) _submit(widget.query);
  }

  @override
  void dispose() {
    _debounce?.cancel();
    _suggestion++;
    unawaited(_repository.cancelSuggestions());
    _results?.dispose();
    _controller.dispose();
    _focus.dispose();
    super.dispose();
  }

  void reset() {
    _debounce?.cancel();
    _suggestion++;
    _controller.clear();
    _results?.dispose();
    _suggestionCache.clear();
    setState(() {
      _results = null;
      _submitted = '';
      _suggestions = const [];
      _searching = false;
    });
  }

  void _submit(String text) {
    final query = text.trim();
    if (query.isEmpty) return;
    final pinyin = widget.store.pinyinSearch;
    final target = pinyin &&
            _suggestions.isNotEmpty &&
            RegExp(r'^[a-zA-Z0-9]+$').hasMatch(query)
        ? _suggestions.first
        : query;
    if (_controller.text != target) _controller.text = target;
    _focus.unfocus();
    _debounce?.cancel();
    _suggestion++;
    unawaited(widget.store.rememberSearch(target).catchError((Object _) {}));
    _results?.dispose();
    final results = _results = DramaFeed(
      load: (page, _) => _catalog.search(target, page: page),
    )..addListener(() => setState(() {}));
    unawaited(results.reload());
    setState(() {
      _submitted = target;
      _suggestions = const [];
    });
  }

  void _changed(String text) {
    _debounce?.cancel();
    final query = text.trim();
    final generation = ++_suggestion;
    if (!widget.source.searchSuggestions || query.isEmpty) {
      setState(() {
        _suggestions = const [];
        _searching = false;
      });
      return;
    }
    if (_suggestionCache.containsKey(query)) {
      setState(() {
        _suggestions = _suggestionCache[query]!;
        _searching = false;
      });
      return;
    }
    setState(() => _searching = true);
    _debounce = Timer(const Duration(milliseconds: 250), () async {
      try {
        final items = await _repository.suggestions(query);
        if (mounted && generation == _suggestion) {
          if (_suggestionCache.length >= 32) {
            _suggestionCache.remove(_suggestionCache.keys.first);
          }
          _suggestionCache[query] = items;
          setState(() {
            _suggestions = items;
            _searching = false;
          });
        }
      } catch (_) {
        if (mounted && generation == _suggestion) {
          setState(() => _searching = false);
        }
      }
    });
  }

  void _open(Drama drama) => openPage<void>(
    context,
    (_) => DetailPage(store: widget.store, drama: drama),
  );

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: widget.store,
    builder: (context, _) {
      final pinyin = widget.store.pinyinSearch;
      final television = AppScope.of(context).television;
      if (television && pinyin) return _televisionPinyin(context);
      final field = SearchField(
        hint: pinyin ? '输入拼音首字母搜索' : '在 ${widget.source.name} 中搜索',
        controller: _controller,
        focusNode: _focus,
        autofocus: widget.query.isEmpty,
        onChanged: _changed,
        onSubmitted: _submit,
      );
      if (television) {
        return Scaffold(
          body: Column(
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(
                  televisionInset,
                  27,
                  televisionInset,
                  0,
                ),
                child: field,
              ),
              Expanded(child: _body(context)),
            ],
          ),
        );
      }
      return Scaffold(
        appBar: AppBar(
          titleSpacing: 0,
          title: Padding(padding: const EdgeInsets.only(right: 16), child: field),
        ),
        body: SafeArea(top: false, bottom: false, child: _body(context)),
      );
    },
  );

  Widget _televisionPinyin(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final query = _controller.text;
    return Scaffold(
      body: Column(
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(televisionInset, 27, televisionInset, 8),
            child: Container(
              height: 48,
              padding: const EdgeInsets.symmetric(horizontal: 16),
              decoration: BoxDecoration(
                color: colors.surfaceContainerHigh,
                borderRadius: BorderRadius.circular(24),
                border: Border.all(color: colors.primary.withValues(alpha: 0.3)),
              ),
              child: Row(
                children: [
                  Icon(Icons.search_rounded, color: colors.primary, size: 24),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Text(
                      query.isEmpty ? '按遥控器输入拼音首字母 (如: BFLC)' : query,
                      style: TextStyle(
                        fontSize: 18,
                        fontWeight: query.isEmpty ? FontWeight.normal : FontWeight.bold,
                        color: query.isEmpty ? colors.onSurfaceVariant : colors.onSurface,
                        letterSpacing: query.isEmpty ? 0 : 2,
                      ),
                    ),
                  ),
                  if (query.isNotEmpty)
                    Text(
                      '${query.length} 字母',
                      style: TextStyle(color: colors.onSurfaceVariant, fontSize: 14),
                    ),
                ],
              ),
            ),
          ),
          Expanded(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(televisionInset, 0, televisionInset, 27),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                spacing: 16,
                children: [
                  SizedBox(
                    width: 360,
                    child: PinyinKeyboard(
                      hasInput: query.isNotEmpty,
                      onKeyPress: (char) {
                        _controller.text += char;
                        _changed(_controller.text);
                      },
                      onBackspace: () {
                        if (query.isNotEmpty) {
                          _controller.text = query.substring(0, query.length - 1);
                          _changed(_controller.text);
                        }
                      },
                      onClear: () {
                        _controller.clear();
                        _changed('');
                      },
                      onSearch: () => _submit(_controller.text),
                    ),
                  ),
                  Expanded(child: _body(context)),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _body(BuildContext context) {
    final television = AppScope.of(context).television;
    final query = _controller.text.trim();
    if (query != _submitted && _searching && _suggestions.isEmpty) {
      return const StatusView.loading(title: '正在匹配...');
    }
    if (_suggestions.isNotEmpty && query != _submitted) {
      final theme = Theme.of(context);
      return ListView(
        padding: television
            ? const EdgeInsets.fromLTRB(televisionInset, 8, televisionInset, 27)
            : const EdgeInsets.symmetric(vertical: 8),
        children: [
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 4),
            child: Text(
              '匹配结果 (${_suggestions.length})',
              style: theme.textTheme.titleSmall?.copyWith(
                fontWeight: FontWeight.bold,
                color: theme.colorScheme.primary,
              ),
            ),
          ),
          for (final item in _suggestions)
            TileFocus(
              builder: (focusNode) => ListTile(
                focusNode: focusNode,
                leading: const Icon(Icons.search_rounded),
                title: Text(item),
                onTap: () => _submit(item),
              ),
            ),
        ],
      );
    }
    final results = _results;
    if (results == null) return _recent(context);
    if (results.items.isEmpty) {
      if (results.loading) return const StatusView.loading(title: '正在搜索');
      if (results.error.isNotEmpty) {
        return StatusView(
          title: '搜索失败',
          message: results.error,
          icon: Icons.cloud_off_rounded,
          action: '重试',
          onAction: () => results.reload(),
        );
      }
      return StatusView(
        title: '没有找到「$_submitted」',
        message: '换个关键词试试',
        icon: Icons.search_off_rounded,
      );
    }
    final inset = television
        ? televisionInset
        : AppScope.formFactorOf(context) == FormFactor.phone
        ? 16.0
        : 24.0;
    return LayoutBuilder(
      builder: (context, constraints) => AutoLoadMore(
        feed: results,
        child: CustomScrollView(
          slivers: [
            SliverPadding(
              padding: EdgeInsets.fromLTRB(
                inset,
                television ? 16 : 8,
                inset,
                0,
              ),
              sliver: DramaSliverGrid(
                width: constraints.maxWidth - inset * 2,
                items: results.items,
                autofocus: television,
                onOpen: _open,
              ),
            ),
            SliverPadding(
              padding: EdgeInsets.fromLTRB(
                inset,
                8,
                inset,
                television ? 27 : 16,
              ),
              sliver: SliverToBoxAdapter(child: FeedFooter(feed: results)),
            ),
          ],
        ),
      ),
    );
  }

  Widget _recent(BuildContext context) => ListenableBuilder(
    listenable: widget.store,
    builder: (context, _) {
      final recent = widget.store.recentSearches;
      if (recent.isEmpty) {
        return StatusView(
          title: '在 ${widget.source.name} 中搜索',
          message: '输入剧名或关键词',
          icon: Icons.search_rounded,
        );
      }
      final theme = Theme.of(context);
      return ListView(
        padding: AppScope.of(context).television
            ? const EdgeInsets.fromLTRB(televisionInset, 8, televisionInset, 27)
            : const EdgeInsets.fromLTRB(16, 8, 16, 24),
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  '最近搜索',
                  style: theme.textTheme.titleSmall?.copyWith(
                    fontWeight: FontWeight.w700,
                  ),
                ),
              ),
              FocusRing(
                radius: 999,
                child: TextButton(
                  onPressed: () => widget.store
                      .clearRecentSearches()
                      .catchError((Object _) {}),
                  child: const Text('清空'),
                ),
              ),
            ],
          ),
          const SizedBox(height: 8),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              for (final query in recent)
                FocusRing(
                  radius: 8,
                  child: ActionChip(
                    avatar: const Icon(Icons.history_rounded, size: 16),
                    label: Text(query),
                    onPressed: () => _submit(query),
                  ),
                ),
            ],
          ),
        ],
      );
    },
  );
}
