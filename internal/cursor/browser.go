package cursor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"cursor-account-admin/internal/model"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

const usagePageURL = "https://www.cursor.com/dashboard?tab=usage"

var (
	browserMu   sync.Mutex
	browserBusy bool
)

const antiDetectionJS = `
Object.defineProperty(navigator, 'webdriver', {get: () => undefined});
if (!window.chrome) window.chrome = {};
if (!window.chrome.runtime) window.chrome.runtime = {};
Object.defineProperty(navigator, 'plugins', {
  get: () => {
    const a=[{name:'Chrome PDF Plugin',filename:'internal-pdf-viewer'},{name:'Native Client',filename:'internal-nacl-plugin'}];
    a.item=(i)=>a[i]; a.namedItem=(n)=>a.find(p=>p.name===n); a.refresh=()=>{};
    return a;
  },
});
Object.defineProperty(navigator, 'languages', {get: () => ['zh-CN','zh','en-US','en']});
`

// scrapeUsageJS extracts usage data from cursor.com/dashboard?tab=usage.
// Supports both English and Chinese (cursor.com/cn/) locale pages.
// Supports both dollar-based (Team Plan: "US$20 / US$20") and request-based ("150 / 500").
const scrapeUsageJS = `
(function() {
  try {
    const result = {
      premium_used: 0, premium_total: 0,
      usage_display: '', on_demand_display: '', reset_date: '', plan: '',
      debug: 'url=' + location.href
    };

    const body = document.body ? (document.body.innerText || '') : '';
    // Dollar regex: supports both "US$20" and "$20" (Chinese locale may omit "US")
    const dollarRe = /(?:US)?\$[\d,.]+/;

    // === Strategy 1: Parse the dashboard cards (.rounded-lg) ===
    const cards = document.querySelectorAll('.rounded-lg');
    result.debug += ' cards=' + cards.length;

    for (let ci = 0; ci < cards.length; ci++) {
      const card = cards[ci];
      const cardText = card.textContent || '';
      const cardLower = cardText.toLowerCase();

      // Debug: dump first 100 chars of each card
      if (ci < 4) {
        result.debug += ' card' + ci + '=' + JSON.stringify(cardText.replace(/\s+/g,' ').substring(0, 120));
      }

      // --- Included usage card ---
      // EN: "included usage", "Your included usage"
      // CN: "包含用量", "已包含", "包含额度", "包含的用量"
      const isIncluded = cardLower.includes('included usage')
        || cardText.includes('包含用量') || cardText.includes('已包含')
        || cardText.includes('包含额度') || cardText.includes('包含的用量');

      if (isIncluded) {
        const amountEls = card.querySelectorAll('[class*="text-xl"]');
        const rawAmounts = [];
        for (const el of amountEls) {
          const t = el.textContent.trim();
          if (t) rawAmounts.push(t);
        }
        result.debug += ' included_raw=' + JSON.stringify(rawAmounts);

        // Try dollar format: "$20" / "/ $20" or "US$20" / "/ US$20"
        const dollars = [];
        for (const t of rawAmounts) {
          const m = t.match(dollarRe);
          if (m) dollars.push(m[0]);
        }
        if (dollars.length >= 2) {
          result.usage_display = dollars[0] + ' / ' + dollars[1];
          const used = parseFloat(dollars[0].replace(/[^0-9.]/g, '')) || 0;
          const total = parseFloat(dollars[1].replace(/[^0-9.]/g, '')) || 0;
          result.premium_used = Math.round(used * 100);
          result.premium_total = Math.round(total * 100);
        } else if (dollars.length === 1) {
          result.usage_display = dollars[0];
          const used = parseFloat(dollars[0].replace(/[^0-9.]/g, '')) || 0;
          result.premium_used = Math.round(used * 100);
        }

        // Try request count: text-xl contains pure numbers like "150", "/ 500"
        if (!result.usage_display) {
          const nums = [];
          for (const t of rawAmounts) {
            const m = t.match(/^[/\s]*(\d+)\s*$/);
            if (m) nums.push(parseInt(m[1], 10));
          }
          if (nums.length >= 2) {
            result.premium_used = nums[0];
            result.premium_total = nums[1];
            result.usage_display = nums[0] + ' / ' + nums[1];
          }
        }

        // Reset date: "Resets 2026年3月5日" or "重置 2026年3月5日"
        const resetMatch = cardText.match(/(?:Resets?|重置)\s*(\d{4}年\d{1,2}月\d{1,2}日)/);
        if (resetMatch) {
          result.reset_date = resetMatch[1];
        }
        if (!result.reset_date) {
          // Fallback: "Resets Apr 5, 2026" or generic text after Reset
          const resetMatch2 = cardText.match(/(?:Resets?|重置)\s+([^\n]{4,30})/);
          if (resetMatch2) {
            result.reset_date = resetMatch2[1].trim().replace(/\s{2,}/g, ' ');
          }
        }
      }

      // --- On-Demand card ---
      // EN: "On-Demand", CN: "按需"
      const isOnDemand = cardLower.includes('on-demand') || cardText.includes('按需');
      if (isOnDemand) {
        const amountEls = card.querySelectorAll('[class*="text-xl"]');
        const odAmounts = [];
        for (const el of amountEls) {
          const m = el.textContent.trim().match(dollarRe);
          if (m) odAmounts.push(m[0]);
        }
        result.debug += ' od_raw=' + JSON.stringify(odAmounts);
        if (odAmounts.length >= 2) {
          result.on_demand_display = odAmounts[0] + ' / ' + odAmounts[1];
        } else if (odAmounts.length === 1) {
          result.on_demand_display = odAmounts[0];
        }
      }
    }

    // === Strategy 2: Fallback - scan ALL .text-xl elements for dollar amounts ===
    if (!result.usage_display && result.premium_total === 0) {
      const allTextXl = document.querySelectorAll('[class*="text-xl"]');
      const allDollars = [];
      const allNums = [];
      const allRawTexts = [];
      for (const el of allTextXl) {
        const t = el.textContent.trim();
        if (t) allRawTexts.push(t.substring(0, 50));
        const dm = t.match(dollarRe);
        if (dm) { allDollars.push(dm[0]); continue; }
        const nm = t.match(/^[/\s]*(\d+)\s*$/);
        if (nm) allNums.push(parseInt(nm[1], 10));
      }
      result.debug += ' s2_raw=' + JSON.stringify(allRawTexts) + ' s2_dollars=' + JSON.stringify(allDollars) + ' s2_nums=' + JSON.stringify(allNums);

      if (allDollars.length >= 2) {
        result.usage_display = allDollars[0] + ' / ' + allDollars[1];
        const used = parseFloat(allDollars[0].replace(/[^0-9.]/g, '')) || 0;
        const total = parseFloat(allDollars[1].replace(/[^0-9.]/g, '')) || 0;
        result.premium_used = Math.round(used * 100);
        result.premium_total = Math.round(total * 100);
        if (allDollars.length >= 4) {
          result.on_demand_display = allDollars[2] + ' / ' + allDollars[3];
        } else if (allDollars.length >= 3) {
          result.on_demand_display = allDollars[2];
        }
      } else if (allNums.length >= 2) {
        result.premium_used = allNums[0];
        result.premium_total = allNums[1];
        result.usage_display = allNums[0] + ' / ' + allNums[1];
      }
    }

    // === Strategy 3: Fallback - scan body text for dollar pair "$N / $M" ===
    if (!result.usage_display && result.premium_total === 0) {
      const dollarPairMatch = body.match(/(?:US)?\$([\d,.]+)\s*[/]\s*(?:US)?\$([\d,.]+)/);
      if (dollarPairMatch) {
        result.usage_display = '$' + dollarPairMatch[1] + ' / $' + dollarPairMatch[2];
        result.premium_used = Math.round(parseFloat(dollarPairMatch[1].replace(/,/g,'')) * 100);
        result.premium_total = Math.round(parseFloat(dollarPairMatch[2].replace(/,/g,'')) * 100);
      }
    }

    // === Reset date fallback: scan body ===
    if (!result.reset_date) {
      const bodyResetMatch = body.match(/(?:Resets?|重置)\s*(\d{4}年\d{1,2}月\d{1,2}日)/);
      if (bodyResetMatch) result.reset_date = bodyResetMatch[1];
    }

    // === Plan detection ===
    const bodyLower = body.toLowerCase();

    // Paid plan keywords
    const paidKeywords = ['team', 'pro', 'business', 'enterprise'];
    for (const p of paidKeywords) {
      if (bodyLower.includes(p + ' plan') || bodyLower.includes(p + ' tier') || bodyLower.includes(p + ' spend')) {
        result.plan = p;
        break;
      }
    }
    if (!result.plan && bodyLower.includes('team spend')) result.plan = 'team';

    // Free / Hobby plan detection (broader matching)
    if (!result.plan) {
      // EN: "free plan", "free tier", "hobby plan", "hobby tier", "upgrade to pro"
      // CN: "免费", "免费方案", "免费计划", "升级到", "升级为"
      const freeIndicators = [
        'free plan', 'free tier', 'hobby plan', 'hobby tier', 'hobby',
        'upgrade to pro', 'upgrade to team', 'upgrade to business',
      ];
      const cnFreeIndicators = ['免费方案', '免费计划', '免费版', '免费套餐'];
      for (const f of freeIndicators) {
        if (bodyLower.includes(f)) { result.plan = 'free'; break; }
      }
      if (!result.plan) {
        for (const f of cnFreeIndicators) {
          if (body.includes(f)) { result.plan = 'free'; break; }
        }
      }
      // If page shows "upgrade" prominently and no paid usage was found → likely free
      if (!result.plan && !result.usage_display && result.premium_total === 0) {
        const hasUpgrade = bodyLower.includes('upgrade') || body.includes('升级');
        if (hasUpgrade) result.plan = 'free';
      }
    }

    // For free/hobby plan: if no usage data was found, set a display value
    if ((result.plan === 'free' || result.plan === 'hobby') && !result.usage_display) {
      result.usage_display = 'Free Plan';
    }

    result.debug += ' body_len=' + body.length;
    return JSON.stringify(result);
  } catch(e) {
    return JSON.stringify({error: e.message, stack: (e.stack||'').substring(0,200)});
  }
})()
`

// LoginResult contains the results of a browser-based login.
type LoginResult struct {
	Cookies string           // Full cookie header string for cursor.com
	Usage   *model.UsageInfo // Parsed usage data (may be nil)
}

// BrowserLogin opens a visible browser window, logs into Cursor, and scrapes usage data.
func BrowserLogin(email, password string) (*LoginResult, error) {
	if err := acquireBrowserLock(); err != nil {
		return nil, err
	}
	defer releaseBrowserLock()

	log.Printf("[browser] 正在打开浏览器，账户: %s", email)

	ctx, cancel, err := newBrowserContext(false)
	if err != nil {
		return nil, err
	}
	defer cancel()

	// Each session uses a fresh temp dir, so no need to clear cookies.
	// Navigate to login page (any cursor.com page triggers login when not authenticated)
	if err := chromedp.Run(ctx, chromedp.Navigate("https://www.cursor.com/settings")); err != nil {
		return nil, fmt.Errorf("无法导航到登录页面: %w", err)
	}

	// Auto-fill the login form
	autoFillLoginForm(ctx, email, password)

	// Wait until login succeeds
	cookies, err := waitForLogin(ctx)
	if err != nil {
		return nil, err
	}

	log.Printf("[browser] 登录成功，账户: %s", email)

	// Navigate to the usage dashboard page
	if err := chromedp.Run(ctx, chromedp.Navigate(usagePageURL)); err != nil {
		log.Printf("[browser] 导航到用量页面失败: %v", err)
	}

	// Wait for the dashboard page to fully render
	waitForDashboardPage(ctx)

	// Scrape usage data (retries internally)
	usage := scrapeUsage(ctx)

	return &LoginResult{Cookies: cookies, Usage: usage}, nil
}

// scrapeUsage retries DOM scraping with delays until valid data is found.
// SPA pages load data asynchronously, so we may need to retry.
// For free/hobby plans without usage numbers, detecting the plan name is sufficient.
func scrapeUsage(ctx context.Context) *model.UsageInfo {
	const maxAttempts = 6

	for i := 0; i < maxAttempts; i++ {
		usage := scrapeUsageFromDOM(ctx)
		if usage != nil && isValidUsage(usage) {
			log.Printf("[browser] DOM 抓取成功 (attempt=%d): display=%s, on_demand=%s, plan=%s",
				i+1, usage.UsageDisplay, usage.OnDemandDisplay, usage.Plan)
			return usage
		}

		if i < maxAttempts-1 {
			log.Printf("[browser] DOM 抓取第 %d 次未获取到数据，等待 SPA 渲染...", i+1)
			_ = chromedp.Run(ctx, chromedp.Sleep(3*time.Second))
		}
	}

	log.Printf("[browser] DOM 抓取 %d 次均未获取到有效数据", maxAttempts)
	return nil
}

// isValidUsage checks if scraped usage data is considered valid.
// For paid plans: must have usage numbers or display string.
// For free/hobby plans: having the plan name detected is sufficient.
func isValidUsage(u *model.UsageInfo) bool {
	if u == nil {
		return false
	}
	if u.PremiumTotal > 0 || u.UsageDisplay != "" {
		return true
	}
	if u.Plan != "" {
		return true
	}
	return false
}

// scrapeUsageFromDOM runs JavaScript to extract usage data from the rendered page.
func scrapeUsageFromDOM(ctx context.Context) *model.UsageInfo {
	scrapeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var resultJSON string
	err := chromedp.Run(scrapeCtx, chromedp.Evaluate(scrapeUsageJS, &resultJSON))
	if err != nil {
		log.Printf("[browser] DOM scrape JS 执行失败: %v", err)
		return nil
	}
	if resultJSON == "" {
		log.Printf("[browser] DOM scrape 返回空结果")
		return nil
	}

	return ParseScrapeResultJSON(resultJSON)
}

// ParseScrapeResultJSON parses the JSON output from scrapeUsageJS into a UsageInfo struct.
// Exported for testing.
func ParseScrapeResultJSON(jsonStr string) *model.UsageInfo {
	var raw struct {
		PremiumUsed     int    `json:"premium_used"`
		PremiumTotal    int    `json:"premium_total"`
		UsageDisplay    string `json:"usage_display"`
		OnDemandDisplay string `json:"on_demand_display"`
		ResetDate       string `json:"reset_date"`
		Plan            string `json:"plan"`
		Debug           string `json:"debug"`
		Error           string `json:"error"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		log.Printf("[browser] DOM scrape 结果解析失败: %v", err)
		return nil
	}
	if raw.Error != "" {
		log.Printf("[browser] DOM scrape JS 错误: %s", raw.Error)
		return nil
	}

	log.Printf("[browser] DOM scrape 原始结果: used=%d, total=%d, display=%s, on_demand=%s, reset=%s, plan=%s, debug=%s",
		raw.PremiumUsed, raw.PremiumTotal, raw.UsageDisplay, raw.OnDemandDisplay, raw.ResetDate, raw.Plan, raw.Debug)

	return &model.UsageInfo{
		PremiumUsed:     raw.PremiumUsed,
		PremiumTotal:    raw.PremiumTotal,
		UsageDisplay:    raw.UsageDisplay,
		OnDemandDisplay: raw.OnDemandDisplay,
		ResetDate:       raw.ResetDate,
		Plan:            raw.Plan,
		FetchedAt:       time.Now(),
	}
}

// waitForDashboardPage waits until the dashboard/usage page has rendered.
// For paid plans: waits for dollar amounts in .text-xl elements.
// For free plans: the page may not contain dollar amounts, so we also accept
// pages that have loaded substantial content with free plan indicators.
func waitForDashboardPage(ctx context.Context) {
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Initial wait for page to start loading
	_ = chromedp.Run(waitCtx, chromedp.Sleep(3*time.Second))

	// JS to check page loading state
	checkDataJS := `(function() {
		const body = (document.body && document.body.innerText) || '';
		const bodyLower = body.toLowerCase();
		const textXlEls = document.querySelectorAll('[class*="text-xl"]');
		let hasDollar = false;
		let textXlTexts = [];
		for (const el of textXlEls) {
			const t = el.textContent.trim();
			if (t) textXlTexts.push(t);
			if (t.includes('$')) hasDollar = true;
		}
		// Detect free/hobby plan indicators
		const isFree = bodyLower.includes('free plan') || bodyLower.includes('hobby')
			|| bodyLower.includes('upgrade to pro') || bodyLower.includes('upgrade to team')
			|| body.includes('免费方案') || body.includes('免费版')
			|| (bodyLower.includes('upgrade') && !hasDollar);
		// Detect usage-related keywords (page has meaningful content)
		const hasUsageContent = bodyLower.includes('usage') || bodyLower.includes('dashboard')
			|| body.includes('用量') || body.includes('设置');
		return JSON.stringify({
			body_len: body.length,
			text_xl_count: textXlEls.length,
			has_dollar: hasDollar,
			is_free: isFree,
			has_usage_content: hasUsageContent,
			text_xl_samples: textXlTexts.slice(0, 6)
		});
	})()`

	for i := 0; i < 12; i++ {
		var resultJSON string
		err := chromedp.Run(waitCtx, chromedp.Evaluate(checkDataJS, &resultJSON))
		if err != nil {
			break
		}

		var check struct {
			BodyLen         int      `json:"body_len"`
			TextXlCount     int      `json:"text_xl_count"`
			HasDollar       bool     `json:"has_dollar"`
			IsFree          bool     `json:"is_free"`
			HasUsageContent bool     `json:"has_usage_content"`
			TextXlSample    []string `json:"text_xl_samples"`
		}
		if json.Unmarshal([]byte(resultJSON), &check) != nil {
			break
		}

		// Best case: .text-xl elements have dollar amounts = paid plan data fully loaded
		if check.HasDollar && check.TextXlCount >= 2 {
			log.Printf("[browser] 页面数据加载完成 (body_len=%d, text_xl=%d, samples=%v, attempt=%d)",
				check.BodyLen, check.TextXlCount, check.TextXlSample, i+1)
			return
		}

		// Free plan: page loaded with meaningful content but no dollar amounts
		if check.IsFree && check.BodyLen > 200 {
			log.Printf("[browser] 检测到 Free 计划页面 (body_len=%d, attempt=%d)",
				check.BodyLen, i+1)
			return
		}

		// Page has substantial content and usage keywords but still no $ (could be free plan)
		if check.HasUsageContent && check.BodyLen > 500 && i >= 3 {
			log.Printf("[browser] 页面内容已加载但无 $ 数据，可能是 Free 计划 (body_len=%d, attempt=%d)",
				check.BodyLen, i+1)
			return
		}

		// Page structure loaded but data not yet rendered - keep waiting
		log.Printf("[browser] 等待数据渲染... (body_len=%d, text_xl=%d, has$=%v, free=%v, samples=%v, attempt=%d)",
			check.BodyLen, check.TextXlCount, check.HasDollar, check.IsFree, check.TextXlSample, i+1)
		_ = chromedp.Run(waitCtx, chromedp.Sleep(2*time.Second))
	}

	log.Printf("[browser] 数据等待超时，继续尝试抓取")
}

// ========== Browser infrastructure ==========

func newBrowserContext(headless bool) (context.Context, context.CancelFunc, error) {
	// Use a unique temp dir for each session to avoid Chrome profile lock conflicts.
	// If the previous Chrome process hasn't fully released its profile lock,
	// chromedp's Allocate fails but leaves a cleanup goroutine that closes
	// c.allocated; a subsequent Run then re-triggers Allocate on the same context,
	// causing "close of closed channel" panic. Unique temp dirs prevent this entirely.
	tempDir, err := os.MkdirTemp("", "cursor-account-admin-chrome-*")
	if err != nil {
		return nil, nil, fmt.Errorf("创建临时目录失败: %w", err)
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", headless),
		chromedp.Flag("enable-automation", false),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("disable-infobars", true),
		chromedp.Flag("disable-extensions", false),
		chromedp.UserDataDir(tempDir),
	)
	if !headless {
		opts = append(opts, chromedp.WindowSize(1000, 750))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, ctxCancel := chromedp.NewContext(allocCtx, chromedp.WithLogf(log.Printf))
	ctx, timeoutCancel := context.WithTimeout(ctx, 5*time.Minute)

	// CRITICAL: run the first action to ensure the browser is fully allocated.
	// If this fails, we must NOT return the context, because the cleanup goroutine
	// in chromedp will close c.allocated, and any subsequent Run on this context
	// would trigger a second Allocate → "close of closed channel" panic.
	runErr := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(antiDetectionJS).Do(ctx)
		return err
	}))
	if runErr != nil {
		timeoutCancel()
		ctxCancel()
		allocCancel()
		time.Sleep(2 * time.Second)
		os.RemoveAll(tempDir)
		return nil, nil, fmt.Errorf("无法启动浏览器，请确保已安装 Chrome 或 Edge: %w", runErr)
	}

	cancel := func() {
		timeoutCancel()
		ctxCancel()   // waits for Chrome DevTools cleanup + c.allocated close
		allocCancel() // waits for allocator goroutines (cmd.Wait) to finish
		// Brief sleep for OS-level process cleanup on Windows
		time.Sleep(1 * time.Second)
		os.RemoveAll(tempDir)
	}
	return ctx, cancel, nil
}

func acquireBrowserLock() error {
	browserMu.Lock()
	defer browserMu.Unlock()
	if browserBusy {
		return fmt.Errorf("另一个浏览器操作正在进行中，请等待完成后再试")
	}
	browserBusy = true
	return nil
}

func releaseBrowserLock() {
	browserMu.Lock()
	browserBusy = false
	browserMu.Unlock()
}

func autoFillLoginForm(ctx context.Context, email, password string) {
	emailCtx, cancel1 := context.WithTimeout(ctx, 15*time.Second)
	defer cancel1()
	err := chromedp.Run(emailCtx,
		chromedp.WaitVisible(`input[type="email"]`, chromedp.ByQuery),
		chromedp.Clear(`input[type="email"]`, chromedp.ByQuery),
		chromedp.SendKeys(`input[type="email"]`, email, chromedp.ByQuery),
		chromedp.Sleep(300*time.Millisecond),
	)
	if err != nil {
		log.Printf("[browser] 未能自动填入邮箱: %v", err)
		return
	}

	btnCtx, cancel2 := context.WithTimeout(ctx, 5*time.Second)
	defer cancel2()
	_ = chromedp.Run(btnCtx, chromedp.Click(`button[type="submit"]`, chromedp.ByQuery))

	passCtx, cancel3 := context.WithTimeout(ctx, 15*time.Second)
	defer cancel3()
	err = chromedp.Run(passCtx,
		chromedp.WaitVisible(`input[type="password"]`, chromedp.ByQuery),
		chromedp.Clear(`input[type="password"]`, chromedp.ByQuery),
		chromedp.SendKeys(`input[type="password"]`, password, chromedp.ByQuery),
		chromedp.Sleep(300*time.Millisecond),
	)
	if err != nil {
		log.Printf("[browser] 未能自动填入密码: %v", err)
		return
	}

	signCtx, cancel4 := context.WithTimeout(ctx, 5*time.Second)
	defer cancel4()
	_ = chromedp.Run(signCtx, chromedp.Click(`button[type="submit"]`, chromedp.ByQuery))
}

func waitForLogin(ctx context.Context) (string, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("登录超时（5分钟），请重试")
		case <-ticker.C:
			var cookies []*network.Cookie
			err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
				var err error
				cookies, err = network.GetCookies().WithURLs([]string{
					"https://www.cursor.com",
					"https://cursor.com",
				}).Do(ctx)
				return err
			}))
			if err != nil {
				if ctx.Err() != nil {
					return "", fmt.Errorf("浏览器已关闭，登录取消")
				}
				continue
			}

			var hasToken bool
			var parts []string
			seen := make(map[string]bool)
			for _, c := range cookies {
				if c.Name == "WorkosCursorSessionToken" && c.Value != "" {
					hasToken = true
				}
				if !seen[c.Name] {
					seen[c.Name] = true
					parts = append(parts, c.Name+"="+c.Value)
				}
			}
			if hasToken {
				log.Printf("[browser] 获取到 %d 个 Cookie", len(parts))
				return strings.Join(parts, "; "), nil
			}
		}
	}
}
