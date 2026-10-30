import { useCallback,useEffect,useRef,useState } from 'react';
import { fetchHotKeywords,fetchShopRanking,fetchSourcingAccounts,generateCopywriting,refreshHotKeywords,searchItems } from './api';
import type { CopywritingMode,HotKeywordSnapshot,RankingEntry,RankingWindow,SearchResultItem,SourcingAccount } from './types';

/** CANCELLED_MESSAGE 是共享请求层在调用方主动取消时抛出的固定文本。 */
const CANCELLED_MESSAGE = '请求已取消';

/** isCancelled 判断错误是否来自调用方主动取消，取消不应展示为失败。 */
export const isCancelled = (error: unknown): boolean => error instanceof Error && error.message === CANCELLED_MESSAGE;

/** errorText 把请求错误转换为用户可见文本，非 Error 时使用调用方提供的兜底文案。 */
export const errorText = (error: unknown, fallback: string): string => (error instanceof Error && error.message ? error.message : fallback);

/** UseSourcingResult 是选品参考页面需要的全部状态与操作。 */
export interface UseSourcingResult {
  /** hot 是当日热词快照；首次加载完成前为 null。 */
  hot: HotKeywordSnapshot | null;
  /** hotError 是热词读取或手动采集失败的原因，空字符串表示无错误。 */
  hotError: string;
  /** refreshingHot 表示是否正在手动采集热词。 */
  refreshingHot: boolean;
  /** refreshHot 手动触发一次热词采集并刷新展示；采集期间重复点击会被忽略。 */
  refreshHot: () => Promise<void>;
  /** accounts 是当前用户名下可用于查货的账号。 */
  accounts: SourcingAccount[];
  /** accountId 是当前选择的查货账号标识。 */
  accountId: string;
  /** setAccountId 切换查货账号。 */
  setAccountId: (id: string) => void;
  /** keyword 是查货输入框中的关键词。 */
  keyword: string;
  /** setKeyword 更新查货关键词。 */
  setKeyword: (value: string) => void;
  /** results 是最近一次成功查货返回的商品。 */
  results: SearchResultItem[];
  /** searchedKeyword 是 results 对应的关键词，空字符串表示尚未查询。 */
  searchedKeyword: string;
  /** searching 表示是否正在查货。 */
  searching: boolean;
  /** searchError 是最近一次查货失败原因。 */
  searchError: string;
  /** search 用当前账号与关键词执行一次查货；可传入关键词直接查询热词。 */
  search: (overrideKeyword?: string) => Promise<void>;
  /** rankingWindow 是畅销榜统计窗口。 */
  rankingWindow: RankingWindow;
  /** setRankingWindow 切换畅销榜统计窗口并自动重新读取。 */
  setRankingWindow: (window: RankingWindow) => void;
  /** ranking 是当前窗口的畅销榜条目。 */
  ranking: RankingEntry[];
  /** rankingLoading 表示是否正在读取畅销榜。 */
  rankingLoading: boolean;
  /** rankingError 是畅销榜读取失败原因。 */
  rankingError: string;
  /** copyMode 是文案模式。 */
  copyMode: CopywritingMode;
  /** setCopyMode 切换文案模式。 */
  setCopyMode: (mode: CopywritingMode) => void;
  /** copyTopic 是文案主题（品类关键词或商品标题）。 */
  copyTopic: string;
  /** setCopyTopic 更新文案主题。 */
  setCopyTopic: (value: string) => void;
  /** copyReference 是文案参考信息。 */
  copyReference: string;
  /** setCopyReference 更新文案参考信息。 */
  setCopyReference: (value: string) => void;
  /** prepareCopywriting 用热词、查货结果或畅销商品预填文案输入，并清空上次结果。 */
  prepareCopywriting: (topic: string, reference: string, mode?: CopywritingMode) => void;
  /** copyContent 是最近一次生成的文案正文。 */
  copyContent: string;
  /** copying 表示是否正在生成文案。 */
  copying: boolean;
  /** copyError 是文案生成失败原因。 */
  copyError: string;
  /** generate 用当前输入生成一次文案。 */
  generate: () => Promise<void>;
}

/** useSourcing 维护选品参考页面的热词、查货、畅销榜与 AI 文案状态；所有请求都可取消且旧响应不会覆盖新结果。 */
export const useSourcing = (): UseSourcingResult => {
  // [hot, setHot] 保存当日热词快照。
  const [hot, setHot] = useState<HotKeywordSnapshot | null>(null);
  // [hotError, setHotError] 保存热词读取或采集失败原因。
  const [hotError, setHotError] = useState('');
  // [refreshingHot, setRefreshingHot] 保存手动采集进行状态。
  const [refreshingHot, setRefreshingHot] = useState(false);
  // [accounts, setAccounts] 保存可用于查货的账号。
  const [accounts, setAccounts] = useState<SourcingAccount[]>([]);
  // [accountId, setAccountId] 保存当前选择的查货账号。
  const [accountId, setAccountId] = useState('');
  // [keyword, setKeyword] 保存查货关键词输入。
  const [keyword, setKeyword] = useState('');
  // [results, setResults] 保存最近一次成功查货的商品。
  const [results, setResults] = useState<SearchResultItem[]>([]);
  // [searchedKeyword, setSearchedKeyword] 保存 results 对应的关键词。
  const [searchedKeyword, setSearchedKeyword] = useState('');
  // [searching, setSearching] 保存查货进行状态。
  const [searching, setSearching] = useState(false);
  // [searchError, setSearchError] 保存查货失败原因。
  const [searchError, setSearchError] = useState('');
  // [rankingWindow, setRankingWindow] 保存畅销榜统计窗口。
  const [rankingWindow, setRankingWindow] = useState<RankingWindow>('7d');
  // [ranking, setRanking] 保存当前窗口的畅销榜条目。
  const [ranking, setRanking] = useState<RankingEntry[]>([]);
  // [rankingLoading, setRankingLoading] 保存畅销榜读取状态。
  const [rankingLoading, setRankingLoading] = useState(false);
  // [rankingError, setRankingError] 保存畅销榜读取失败原因。
  const [rankingError, setRankingError] = useState('');
  // [copyMode, setCopyMode] 保存文案模式。
  const [copyMode, setCopyMode] = useState<CopywritingMode>('listing');
  // [copyTopic, setCopyTopic] 保存文案主题。
  const [copyTopic, setCopyTopic] = useState('');
  // [copyReference, setCopyReference] 保存文案参考信息。
  const [copyReference, setCopyReference] = useState('');
  // [copyContent, setCopyContent] 保存最近一次生成的文案。
  const [copyContent, setCopyContent] = useState('');
  // [copying, setCopying] 保存文案生成状态。
  const [copying, setCopying] = useState(false);
  // [copyError, setCopyError] 保存文案生成失败原因。
  const [copyError, setCopyError] = useState('');

  // hotControllerRef 保存当前热词读取的取消器，卸载或重新读取时取消旧请求。
  const hotControllerRef = useRef<AbortController | null>(null);
  // refreshingRef 同步记录手动采集是否进行中，避免连点在状态提交前发出多次采集。
  const refreshingRef = useRef(false);
  // searchControllerRef 保存当前查货请求的取消器。
  const searchControllerRef = useRef<AbortController | null>(null);
  // searchGenerationRef 是查货请求代次，只有最新代次的响应可以写入状态。
  const searchGenerationRef = useRef(0);
  // copyControllerRef 保存当前文案生成请求的取消器。
  const copyControllerRef = useRef<AbortController | null>(null);
  // copyGenerationRef 是文案请求代次，只有最新代次的响应可以写入状态。
  const copyGenerationRef = useRef(0);

  // loadHot 读取当日热词快照，并取消上一次未完成的读取。
  const loadHot = useCallback(/* loadHotAction 读取热词并写入状态。 */ async () => {
    hotControllerRef.current?.abort();
    // controller 是本次热词读取独占的取消器。
    const controller = new AbortController();
    hotControllerRef.current = controller;
    try {
      // snapshot 是服务端返回的热词快照。
      const snapshot = await fetchHotKeywords({ signal: controller.signal });
      if (controller.signal.aborted) return;
      setHot(snapshot);
    } catch (error /* error 是热词读取失败原因。 */) {
      if (controller.signal.aborted || isCancelled(error)) return;
      setHotError(errorText(error, '读取平台热词失败'));
    }
  }, []);

  useEffect(/* 页面挂载时读取热词，卸载时取消。 */ () => {
    void loadHot();
    return /* 卸载时取消热词读取。 */ () => hotControllerRef.current?.abort();
  }, [loadHot]);

  useEffect(/* 页面挂载时读取账号列表，与热词读取并行。 */ () => {
    // controller 是本次账号读取独占的取消器。
    const controller = new AbortController();
    fetchSourcingAccounts({ signal: controller.signal })
      .then(/* list 是当前用户名下账号。 */ list => {
        if (controller.signal.aborted) return;
        setAccounts(list);
        setAccountId(/* previous 是已有选择，未选择时默认第一个账号。 */ previous => previous || (list[0]?.id ?? ''));
      })
      .catch(/* error 是账号读取失败原因；账号为空时页面会提示先添加账号。 */ (error: unknown) => {
        if (controller.signal.aborted || isCancelled(error)) return;
        setSearchError(errorText(error, '读取账号列表失败'));
      });
    return /* 卸载时取消账号读取。 */ () => controller.abort();
  }, []);

  useEffect(/* 统计窗口变化时重新读取畅销榜，旧窗口的响应会被丢弃。 */ () => {
    // controller 是本窗口畅销榜读取独占的取消器。
    const controller = new AbortController();
    setRankingLoading(true);
    setRankingError('');
    fetchShopRanking(rankingWindow, { signal: controller.signal })
      .then(/* data 是当前窗口的畅销榜。 */ data => {
        if (controller.signal.aborted) return;
        setRanking(data.entries);
        setRankingLoading(false);
      })
      .catch(/* error 是畅销榜读取失败原因。 */ (error: unknown) => {
        if (controller.signal.aborted || isCancelled(error)) return;
        setRanking([]);
        setRankingError(errorText(error, '读取本店畅销榜失败'));
        setRankingLoading(false);
      });
    return /* 窗口切换或卸载时取消旧读取。 */ () => controller.abort();
  }, [rankingWindow]);

  useEffect(/* 卸载时取消仍在进行的查货与文案请求，避免写入已卸载组件。 */ () => /* cancelPendingOnUnmount 卸载时取消查货与文案请求。 */ () => {
    searchControllerRef.current?.abort();
    copyControllerRef.current?.abort();
  }, []);

  // refreshHot 手动触发一次游客态热词采集，成功或失败后都重新读取快照以展示服务端记录的状态。
  const refreshHot = useCallback(/* refreshHotAction 执行一次手动采集。 */ async () => {
    if (refreshingRef.current) return;
    refreshingRef.current = true;
    setRefreshingHot(true);
    setHotError('');
    try {
      await refreshHotKeywords();
    } catch (error /* error 是采集失败原因，失败快照已由服务端记录。 */) {
      if (!isCancelled(error)) setHotError(errorText(error, '采集平台热词失败'));
    } finally {
      refreshingRef.current = false;
      setRefreshingHot(false);
    }
    await loadHot();
  }, [loadHot]);

  // search 执行一次查货；新查询会取消旧查询，旧响应不会覆盖新结果。
  const search = useCallback(/* searchAction 执行一次关键词查货。 */ async (overrideKeyword?: string) => {
    // term 是本次实际查询的关键词，优先使用热词点击传入的值。
    const term = (overrideKeyword ?? keyword).trim();
    if (!term) {
      setSearchError('请输入要查询的关键词');
      return;
    }
    if (!accountId) {
      setSearchError('请先在账号管理中添加并选择一个闲鱼账号');
      return;
    }
    if (overrideKeyword !== undefined) setKeyword(term);
    searchControllerRef.current?.abort();
    // controller 是本次查货独占的取消器。
    const controller = new AbortController();
    searchControllerRef.current = controller;
    searchGenerationRef.current += 1;
    // generation 是本次查货的代次。
    const generation = searchGenerationRef.current;
    setSearching(true);
    setSearchError('');
    try {
      // items 是本次查货返回的商品。
      const items = await searchItems(accountId, term, { signal: controller.signal });
      if (generation !== searchGenerationRef.current) return;
      setResults(items);
      setSearchedKeyword(term);
    } catch (error /* error 是查货失败原因，包括冷却、风控与登录失效。 */) {
      if (generation !== searchGenerationRef.current || isCancelled(error)) return;
      setSearchError(errorText(error, '查询商品失败'));
    } finally {
      if (generation === searchGenerationRef.current) setSearching(false);
    }
  }, [accountId, keyword]);

  // prepareCopywriting 预填文案输入并清空上次结果，用于从热词、查货结果或畅销商品快速发起。
  const prepareCopywriting = useCallback(/* prepareAction 预填文案输入。 */ (topic: string, reference: string, mode?: CopywritingMode) => {
    copyControllerRef.current?.abort();
    copyGenerationRef.current += 1;
    setCopyTopic(topic);
    setCopyReference(reference);
    if (mode) setCopyMode(mode);
    setCopyContent('');
    setCopyError('');
    setCopying(false);
  }, []);

  // generate 用当前输入生成一次文案；新请求会取消旧请求。
  const generate = useCallback(/* generateAction 生成一次文案。 */ async () => {
    // topic 是去空白后的文案主题。
    const topic = copyTopic.trim();
    if (!topic) {
      setCopyError('请先填写品类关键词或商品标题');
      return;
    }
    copyControllerRef.current?.abort();
    // controller 是本次文案生成独占的取消器。
    const controller = new AbortController();
    copyControllerRef.current = controller;
    copyGenerationRef.current += 1;
    // generation 是本次文案请求的代次。
    const generation = copyGenerationRef.current;
    setCopying(true);
    setCopyError('');
    try {
      // content 是 AI 生成的文案正文。
      const content = await generateCopywriting({ mode: copyMode, topic, reference: copyReference.trim() }, { signal: controller.signal });
      if (generation !== copyGenerationRef.current) return;
      setCopyContent(content);
    } catch (error /* error 是文案生成失败原因，例如 AI 未配置。 */) {
      if (generation !== copyGenerationRef.current || isCancelled(error)) return;
      setCopyContent('');
      setCopyError(errorText(error, '生成文案失败'));
    } finally {
      if (generation === copyGenerationRef.current) setCopying(false);
    }
  }, [copyMode, copyReference, copyTopic]);

  return {
    hot, hotError, refreshingHot, refreshHot,
    accounts, accountId, setAccountId,
    keyword, setKeyword, results, searchedKeyword, searching, searchError, search,
    rankingWindow, setRankingWindow, ranking, rankingLoading, rankingError,
    copyMode, setCopyMode, copyTopic, setCopyTopic, copyReference, setCopyReference, prepareCopywriting,
    copyContent, copying, copyError, generate,
  };
};
