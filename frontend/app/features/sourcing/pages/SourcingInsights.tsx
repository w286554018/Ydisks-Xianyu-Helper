import React,{ useRef } from 'react';
import { PageHeader } from '../../../../shared/ui/PageHeader';
import { CopywritingPanel } from '../components/CopywritingPanel';
import { HotKeywordPanel } from '../components/HotKeywordPanel';
import { ItemSearchPanel } from '../components/ItemSearchPanel';
import { ShopRankingPanel } from '../components/ShopRankingPanel';
import { useSourcing } from '../hooks';
import { searchItemReference } from '../models';

/** SourcingInsights 是选品参考页面：平台热词、关键词查货、我的畅销榜与 AI 文案。 */
const SourcingInsights: React.FC = () => {
  // sourcing 是页面全部状态与操作。
  const sourcing = useSourcing();
  // copywritingRef 指向文案面板，用于从其它面板发起文案时滚动到可见区域。
  const copywritingRef = useRef<HTMLDivElement | null>(null);

  // focusCopywriting 把文案面板滚动到可见区域。
  const focusCopywriting = () => copywritingRef.current?.scrollIntoView?.({ behavior: 'smooth', block: 'start' });

  // copyText 把文本写入剪贴板；浏览器不允许时提示用户手动复制。
  const copyText = (text: string) => {
    if (!text) return;
    navigator.clipboard?.writeText(text).catch(/* 复制失败时提示手动选择文本。 */ () => alert('复制失败，请手动选择文本复制'));
  };

  return (
    <div className="space-y-6">
      <PageHeader title="选品参考" description="看平台热词、查在售商品、对照自己的畅销榜，再让 AI 写上架文案。" />
      <HotKeywordPanel
        hot={sourcing.hot}
        error={sourcing.hotError}
        refreshing={sourcing.refreshingHot}
        onRefresh={/* refreshHot 手动采集热词。 */ () => void sourcing.refreshHot()}
        onSearch={/* searchHot 用热词查货。 */ keyword => void sourcing.search(keyword)}
        onAnalyze={/* analyzeHot 用热词预填品类分析。 */ keyword => { sourcing.prepareCopywriting(keyword, '', 'analysis'); focusCopywriting(); }}
      />
      <div className="grid grid-cols-1 gap-6 xl:grid-cols-5">
        <div className="space-y-6 xl:col-span-3">
          <ItemSearchPanel
            accounts={sourcing.accounts}
            accountId={sourcing.accountId}
            onAccountChange={sourcing.setAccountId}
            keyword={sourcing.keyword}
            onKeywordChange={sourcing.setKeyword}
            results={sourcing.results}
            searchedKeyword={sourcing.searchedKeyword}
            searching={sourcing.searching}
            error={sourcing.searchError}
            onSearch={/* runSearch 用输入框关键词查货。 */ () => void sourcing.search()}
            onWriteCopy={/* writeFromItem 用查货结果预填上架文案。 */ item => { sourcing.prepareCopywriting(item.title, searchItemReference(item), 'listing'); focusCopywriting(); }}
          />
          <ShopRankingPanel
            window={sourcing.rankingWindow}
            onWindowChange={sourcing.setRankingWindow}
            entries={sourcing.ranking}
            loading={sourcing.rankingLoading}
            error={sourcing.rankingError}
            onWriteCopy={/* writeFromRanking 用畅销商品预填上架文案。 */ entry => { sourcing.prepareCopywriting(entry.itemTitle || entry.itemId, `本店近期售出 ${entry.soldQuantity} 件`, 'listing'); focusCopywriting(); }}
          />
        </div>
        <div ref={copywritingRef} className="xl:col-span-2">
          <CopywritingPanel
            mode={sourcing.copyMode}
            onModeChange={sourcing.setCopyMode}
            topic={sourcing.copyTopic}
            onTopicChange={sourcing.setCopyTopic}
            reference={sourcing.copyReference}
            onReferenceChange={sourcing.setCopyReference}
            content={sourcing.copyContent}
            generating={sourcing.copying}
            error={sourcing.copyError}
            onGenerate={/* runGenerate 生成一次文案。 */ () => void sourcing.generate()}
            onCopy={copyText}
          />
        </div>
      </div>
    </div>
  );
};

export default SourcingInsights;
