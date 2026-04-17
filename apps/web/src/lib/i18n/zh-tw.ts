export const LOCALE_CODE = "zh-TW" as const;

export const zhTW = {
  app: {
    name: "企業訂餐平台",
    subtitle: "共享前端平台與體驗基線"
  },
  shell: {
    navLabel: "入口導覽",
    globalLoading: "頁面更新中",
    offlineTitle: "目前離線",
    offlineDescription: "網路恢復後會自動重新同步最新資訊。",
    actorLabel: "登入身分",
    providerLabel: "驗證來源",
    refreshAfterLabel: "更新門檻",
    expiresAtLabel: "到期時間",
    notSignedIn: "尚未登入",
    signOut: "登出"
  },
  home: {
    title: "單一入口，依角色切換工作流",
    description:
      "同一套前端應用提供員工、商家與福委會入口，導覽、版型與狀態呈現依角色自動調整。",
    portalPreviewLabel: "入口焦點",
    continueToPortal: "前往目前入口",
    signInActionsLabel: "快速登入（開發環境）",
    signIn: {
      employee: "以員工身分登入",
      vendor: "以商家身分登入",
      admin: "以福委會身分登入"
    },
    probeApi: "檢查 API 連線",
    actions: {
      idle: "可直接切換身分或檢查後端連線。",
      loading: "請求處理中，已暫停重複送出。",
      successFallback: "操作已完成。",
      errorFallback: "操作失敗，請稍後再試。"
    },
    clearSession: "清除目前工作階段",
    activeSessionTitle: "目前工作階段"
  },
  nav: {
    portalLinksLabel: "入口切換",
    sectionLinksLabel: "功能分區",
    lockedHint: "目前角色不可存取此入口",
    portals: {
      employee: "員工入口",
      vendor: "商家入口",
      admin: "福委會入口"
    },
    sections: {
      employee: {
        overview: "總覽",
        orders: "訂單",
        payroll: "薪資扣款"
      },
      vendor: {
        overview: "總覽",
        fulfillment: "履約配送",
        menu: "菜單與供應",
        docs: "文件與追蹤"
      },
      admin: {
        overview: "總覽",
        vendors: "商家審核",
        anomalies: "異常治理"
      }
    }
  },
  portal: {
    employee: {
      title: "員工入口",
      lead: "手機優先，快速完成多日預購與領餐流程。",
      sectionDescriptions: {
        overview: "追蹤今日餐點狀態與提醒。",
        orders: "管理預購、修改與取消訂單。",
        payroll: "查看薪資扣款與申訴進度。"
      }
    },
    vendor: {
      title: "商家入口",
      lead: "桌機優先，穩定處理備餐與配送流程。",
      sectionDescriptions: {
        overview: "檢視當日作業摘要。",
        fulfillment: "依廠區追蹤履約與配送狀態。",
        menu: "維護菜單、供應量與截單規則。",
        docs: "管理文件上傳、存取連結與 objectRef 追蹤。"
      }
    },
    admin: {
      title: "福委會入口",
      lead: "桌機優先，集中完成審核、稽核與治理。",
      sectionDescriptions: {
        overview: "掌握平台營運與稽核訊號。",
        vendors: "管理商家審核與服務範圍。",
        anomalies: "處理文件、品質與服務異常。"
      }
    }
  },
  portalSurface: {
    actorSummaryLabel: "當前執行身分",
    providerSummaryLabel: "目前驗證供應商",
    experienceLabel: "版型模式",
    experience: {
      mobileFirst: "手機優先（員工體驗）",
      desktopFirst: "桌機優先（商家 / 福委會體驗）"
    },
    platformReady: "共享平台基線已載入，可直接延伸各入口功能模組。"
  },
  api: {
    probe: {
      success: "後端連線檢查完成。"
    },
    failure: {
      baseUrlMissing: "尚未設定 PUBLIC_API_BASE_URL，無法連線後端服務。",
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
