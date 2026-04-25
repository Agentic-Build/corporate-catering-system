export const LOCALE_CODE = "zh-TW" as const;

export const zhTW = {
  app: {
    name: "企業訂餐平台",
    subtitle: "為員工、商家、福委會量身設計的任務導向工作流"
  },
  shell: {
    navLabel: "主導覽",
    globalLoading: "頁面更新中",
    offlineTitle: "目前離線",
    offlineDescription: "網路恢復後會自動重新同步。",
    actorLabel: "登入身分",
    providerLabel: "驗證來源",
    refreshAfterLabel: "更新門檻",
    expiresAtLabel: "到期時間",
    notSignedIn: "尚未登入",
    signOut: "登出",
    lockedPortal: "目前角色不可存取此入口",
    openMenu: "開啟選單",
    closeMenu: "關閉選單"
  },
  home: {
    title: "先登入，再依角色切換工作流",
    description:
      "選擇你的身分，系統會直接帶你進入最常用的任務頁面。員工手機優先，商家與福委會桌機優先。",
    rolePreviewLabel: "角色預覽",
    signInActionsLabel: "快速登入",
    signIn: {
      employee: "以員工身分登入",
      vendor: "以商家身分登入",
      admin: "以福委會身分登入"
    },
    probeApi: "檢查 API 連線",
    continueToPortal: "前往目前入口",
    clearSession: "清除工作階段",
    activeSessionTitle: "目前工作階段",
    actions: {
      idle: "可直接切換身分或檢查後端連線。",
      loading: "請求處理中，已暫停重複送出。",
      successFallback: "操作已完成。",
      errorFallback: "操作失敗，請稍後再試。"
    }
  },
  nav: {
    portalLinksLabel: "入口切換",
    sectionLinksLabel: "功能分區",
    portals: {
      employee: "員工入口",
      vendor: "商家入口",
      admin: "福委會入口"
    },
    employee: {
      home: "今日",
      homeDesc: "當日餐點、待領取、提醒",
      discover: "菜單",
      discoverDesc: "瀏覽、搜尋、下單",
      orders: "訂單",
      ordersDesc: "管理、修改、領餐",
      wallet: "扣款",
      walletDesc: "明細與申訴"
    },
    vendor: {
      today: "今日",
      todayDesc: "備餐看板、配送狀態",
      menu: "菜單",
      menuDesc: "維護菜單項目",
      schedule: "訂購政策",
      scheduleDesc: "預購開放 / 截單",
      batches: "備餐批次",
      batchesDesc: "總表、分區、標籤、配送籃",
      orders: "訂單",
      ordersDesc: "營運查詢",
      compliance: "合規",
      complianceDesc: "文件與合規狀態",
      insights: "分析",
      insightsDesc: "營運指標"
    },
    admin: {
      overview: "總覽",
      overviewDesc: "統一 Inbox、待辦",
      vendors: "商家審核",
      vendorsDesc: "清單、審核、映射",
      compliance: "合規文件",
      complianceDesc: "模板、生命週期",
      settlement: "月結",
      settlementDesc: "關帳、爭議、鎖帳",
      anomalies: "異常治理",
      anomaliesDesc: "告警、規則、評估",
      audit: "稽核",
      auditDesc: "操作留痕、責任歸屬",
      analytics: "營運分析",
      analyticsDesc: "跨商家指標儀表板"
    }
  },
  taskHeader: {
    back: "返回"
  },
  common: {
    loading: "載入中",
    retry: "重試",
    refresh: "重新整理",
    submit: "送出",
    save: "儲存",
    cancel: "取消",
    confirm: "確認",
    remove: "移除",
    edit: "編輯",
    close: "關閉",
    empty: "尚無資料",
    required: "必填",
    optional: "選填",
    pageLoading: "同步最新資料中",
    errorTitle: "載入失敗",
    tryLater: "請稍後再試或回報管理員。"
  },
  employee: {
    home: {
      greeting: (name: string) => `您好，${name}`,
      todaySectionTitle: "今日待領取",
      todayEmpty: "今日沒有待領取訂單，去逛菜單吧！",
      upcomingSectionTitle: "即將截單",
      upcomingEmpty: "近期沒有即將截單的預購。",
      walletSectionTitle: "帳務摘要",
      quickActions: "快速動作",
      actions: {
        discover: "瀏覽菜單",
        orders: "查看所有訂單",
        wallet: "查看扣款"
      }
    },
    discover: {
      title: "瀏覽菜單並下單",
      description: "週檢視適合一次規劃一整週；日曆檢視適合指定日期範圍。",
      viewToggle: "檢視模式",
      view: {
        week: "週檢視",
        calendar: "日曆檢視"
      },
      weekStart: "週起始日",
      fromDate: "起始日",
      toDate: "結束日",
      globalNote: "訂單備註（本次下單共用）",
      applyFilters: "套用條件",
      card: {
        remaining: (n: number) => `剩餘 ${n} 份`,
        closed: "已關閉",
        soldOut: "售完",
        cutoffLabel: "截單"
      },
      placeOrder: "立即下單",
      quantity: "數量",
      emptyTitle: "指定條件內沒有符合的菜單",
      emptyDescription: "調整日期範圍或切換檢視模式再試一次。"
    },
    orders: {
      title: "我的訂單",
      description: "列出所有預購中、已完成、已取消的訂單。",
      filters: {
        status: "狀態",
        all: "全部",
        from: "起始日",
        to: "結束日"
      },
      empty: "尚無訂單，前往菜單先下一筆吧。",
      rowActions: {
        view: "詳情",
        edit: "修改",
        cancel: "取消",
        pickup: "顯示領餐 QR",
        dispute: "提交申訴"
      }
    },
    orderDetail: {
      title: "訂單詳情",
      lineItems: "品項",
      timeline: "訂單狀態軌跡",
      totalLabel: "訂單金額",
      createdAtLabel: "建立時間",
      deliveryDateLabel: "配送日",
      eligiblePickup: "可顯示領餐 QR",
      notEligiblePickup: "目前狀態不可領餐"
    },
    edit: {
      title: "修改訂單",
      description: "調整各品項數量，超過截單時間將無法變更。",
      submit: "送出修改",
      rule: "每品項 1–20 份。整張訂單至少需保留 1 筆品項。"
    },
    cancel: {
      title: "取消訂單",
      description: "截單前可取消訂單，取消後系統會產生對應退款流水。",
      reasonLabel: "取消原因（至少 5 字）",
      confirmTitle: "確定要取消此訂單？",
      confirmBody: "取消後狀態會變更為 CANCELLED，且無法還原。",
      submit: "確認取消"
    },
    pickup: {
      title: "領餐驗證 QR",
      description: "QR 每 30 秒自動更新。到領餐點出示並請現場人員掃描。",
      refreshNow: "立即刷新 QR",
      complete: "完成領餐核銷",
      refreshCountdown: (s: number) => `QR 更新倒數 ${s} 秒`,
      ineligible: "此訂單目前不可領餐，請回訂單列表確認狀態。"
    },
    dispute: {
      title: "提交扣款申訴",
      description: "若此訂單有扣款錯誤或異議，請附上說明。",
      reasonLabel: "申訴原因",
      submit: "送出申訴",
      trackerTitle: "申訴追蹤",
      statusLabel: "狀態",
      ownerLabel: "負責人",
      emptyTrace: "尚無申訴紀錄"
    },
    wallet: {
      title: "薪資扣款與申訴",
      description: "左側選擇訂單，右側檢視明細與申訴進度。",
      summary: {
        loadedOrders: "已載入訂單",
        netAmount: "淨扣款總額",
        openDisputes: "進行中申訴",
        resolvedDisputes: "已結案申訴"
      },
      pickPromptTitle: "請從左側選擇訂單",
      pickPromptBody: "系統會載入對應的扣款流水與申訴追蹤。"
    }
  },
  vendor: {
    today: {
      title: "今日作業儀表板",
      description: "總覽備餐份數、配送狀態與需立即處理的異常。",
      summary: {
        toPrepare: "今日備餐",
        toDeliver: "待上車",
        delivered: "已送達"
      },
      upcomingCutoff: "即將截單",
      quickActions: "快速動作",
      actions: {
        openBoard: "進入今日看板",
        createBatch: "建立備餐批次",
        updateMenu: "更新菜單"
      }
    },
    board: {
      title: "今日配送看板",
      description: "按日期與廠區查看訂單與推進配送狀態。",
      filters: {
        deliveryDate: "配送日",
        plant: "廠區",
        includeAudit: "顯示配送稽核軌跡"
      }
    },
    menu: {
      listTitle: "菜單總覽",
      listDescription: "列出所有菜單項目，可依日期 / 狀態 / 排序篩選。",
      createTitle: "新增菜單",
      editTitle: "編輯菜單",
      editDescription: "修改會直接生效並同步至員工端顯示。",
      statusAction: {
        listed: "上架 (LISTED)",
        paused: "暫停 (PAUSED)",
        delisted: "永久下架 (DELISTED)"
      }
    },
    schedule: {
      title: "訂購政策",
      description: "設定全店預購開放天數與前日截單時間；每筆菜單可另行 override。",
      preorderOpenDaysAhead: "預購開放天數（1–7）",
      modifyCancelCutoff: "前日截單時間（分鐘 900–1200）",
      submit: "更新政策"
    },
    batches: {
      listTitle: "備餐批次",
      listDescription: "批次建立後即為不可變快照，可隨時重印。",
      detailTitle: "批次詳情",
      create: "建立今日批次",
      print: "列印批次",
      lookupLabel: "批次編號",
      recentLabel: "最近批次"
    },
    orders: {
      title: "營運訂單查詢",
      description: "按日期範圍、狀態、廠區查詢訂單並推進配送。",
      statusLabel: "訂單狀態"
    },
    compliance: {
      title: "合規狀態",
      description: "檢視商家合規狀態、上傳文件與建立下載連結。",
      uploadTitle: "建立上傳計畫",
      accessLinkTitle: "建立下載連結",
      objectRefLabel: "objectRef"
    },
    insights: {
      title: "營運分析",
      description: "指定日期範圍檢視本店營運指標。"
    }
  },
  admin: {
    overview: {
      title: "統一 Inbox",
      description: "一頁掌握今天要處理的治理任務。",
      tiles: {
        pendingVendors: "待審商家",
        openAnomalies: "開放告警",
        slaBreached: "SLA 超時",
        payrollExceptions: "月結例外",
        disputes: "爭議待處理"
      },
      quickActions: {
        closeSettlement: "執行月結關帳",
        runLifecycle: "執行合規生命週期",
        evaluateAnomaly: "評估異常規則"
      }
    },
    vendors: {
      listTitle: "商家清單",
      listDescription: "審核、檢視文件、管理廠區映射皆由此進入。",
      detailTitle: "商家詳情",
      reviewTitle: "審核決策",
      reviewDescription: "意見需至少 5 字。決策會寫入 append-only 歷程。",
      mappingTitle: "廠區映射",
      mappingDescription: "ALLOW / DENY 規則；優先級越小越先生效。"
    },
    compliance: {
      templatesTitle: "合規文件模板",
      templatesDescription: "依商家分類定義必交文件與到期邏輯。",
      lifecycleTitle: "執行合規生命週期",
      lifecycleDescription: "自動發送提醒、停權逾期、復權補件完成。"
    },
    settlement: {
      hubTitle: "月結作業",
      hubDescription: "執行關帳、管理週期鎖定、處理爭議。",
      closeTitle: "執行月結關帳",
      closeDescription: "需簽核 ISS-003 後才可提交。",
      cyclesTitle: "結算週期",
      cycleDetailTitle: "週期詳情",
      disputesTitle: "月結爭議",
      disputeDetailTitle: "爭議處理",
      lockAction: "鎖定週期",
      unlockAction: "解鎖週期"
    },
    anomalies: {
      listTitle: "異常告警",
      listDescription: "查詢、推進狀態、結束告警（需 ISS-007 簽核）。",
      detailTitle: "告警詳情",
      evaluateTitle: "評估異常規則",
      rulesTitle: "異常規則",
      ruleDetailTitle: "規則詳情"
    },
    audit: {
      title: "稽核查詢",
      description: "查詢操作留痕與責任歸屬。所有寫入皆為 append-only。",
      responsibilitiesTitle: "責任歸屬"
    },
    analytics: {
      title: "營運分析儀表板",
      description: "跨商家、廠區、時間的營運指標。"
    }
  },
  api: {
    probe: {
      success: "後端連線檢查完成。"
    },
    failure: {
      baseUrlMissing: "PUBLIC_API_BASE_URL 未設定或格式錯誤，無法連線後端服務。",
      plantScopeMissing: "目前角色缺少廠區範圍設定，無法執行 API 連線檢查。",
      network: "無法連線後端服務，請確認網路或稍後再試。",
      unknown: "發生未預期的 API 錯誤，請稍後再試。",
      statusText: {
        400: "API 請求格式錯誤。",
        401: "API 驗證失敗，請重新登入。",
        403: "API 權限不足。",
        404: "API 路徑不存在。",
        409: "API 請求與目前狀態衝突。",
        422: "API 驗證規則未通過。",
        500: "後端服務發生內部錯誤。",
        502: "後端上游服務暫時不可用。",
        503: "後端服務維護中。"
      }
    }
  },
  asyncState: {
    idle: "尚未開始同步資料。",
    loading: "同步平台資料中。",
    success: "共用平台設定已生效。",
    error: "平台資料同步失敗，請稍後再試。"
  },
  error: {
    title: "系統暫時無法完成請求",
    statusCodePrefix: "狀態碼",
    genericDescription: "請稍後重試；若問題持續，請聯絡系統管理員。",
    statusText: {
      400: "請求格式錯誤",
      401: "尚未登入或工作階段已失效",
      403: "你目前沒有此頁面的存取權限",
      404: "找不到對應頁面",
      500: "系統內部錯誤",
      502: "上游服務暫時不可用",
      503: "服務維護中"
    },
    backHome: "回到首頁"
  }
} as const;

export type LocaleCode = typeof LOCALE_CODE;
