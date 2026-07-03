export default {
  nav: {
    features: '核心特性',
    integrations: '生态集成',
    documentation: '开发文档',
    pricing: '价格方案',
    signIn: '登录',
    getStarted: '立即开始'
  },
  hero: {
    badge: 'TransitHub 2.0 震撼发布',
    title: '终极版',
    highlight: 'API 流量网关',
    subtitle: '统一接管你的 NewAPI 实例，轻松管理密钥并智能调度流量。专为现代 AI 基础设施而生。',
    startBtn: '立即开始构建',
    docBtn: '查阅开发文档'
  },
  features: {
    title: '为性能与规模而设计',
    subtitle: '跨分布式网络管理海量 API 流量所需的一切，全部打包在这个极具美感的控制台中。',
    items: {
      sync: {
        title: '多实例自动同步',
        desc: '在多个 NewAPI 实例间无缝同步，零停机时间，并自动解决配置冲突。'
      },
      fallback: {
        title: '智能降级调度',
        desc: '智能路由与自动降级机制确保即使单个供应商宕机，你的 API 请求也绝不失败。'
      },
      observe: {
        title: '全局可观测性',
        desc: '在全球范围内实时监控你所有的 API 密钥状态、额度消耗以及延迟指标。'
      },
      selfhost: {
        title: '自托管就绪',
        desc: '随处部署。全面支持 Docker、Kubernetes 以及裸金属 VPS 安装。'
      }
    }
  },
  cta: {
    title: '准备好接管一切了吗？',
    subtitle: '加入成千上万使用 TransitHub 驱动其 API 基础设施的开发者行列。今天就免费开始吧。',
    deployBtn: '立即部署',
    salesBtn: '联系销售'
  },
  footer: {
    rights: 'TransitHub 运维团队。保留所有权利。'
  },
  auth: {
    backToHome: '返回主页',
    login: {
      title: '欢迎回来',
      subtitle: '输入您的邮箱和密码登录 TransitHub',
      email: '邮箱',
      emailPlaceholder: "name{'@'}example.com",
      password: '密码',
      passwordPlaceholder: '输入密码',
      submit: '登录',
      submitting: '登录中...',
      success: '登录成功，正在打开管理后台...',
      errors: {
        login: '登录失败，请检查邮箱和密码后重试。'
      },
      noAccount: '还没有账号？',
      registerLink: '去注册'
    },
    register: {
      title: '创建账号',
      subtitle: '输入您的信息以注册 TransitHub',
      email: '邮箱',
      emailPlaceholder: "name{'@'}example.com",
      password: '密码',
      passwordPlaceholder: '设置密码',
      code: '验证码',
      codePlaceholder: '输入 6 位验证码',
      sendCode: '发送验证码',
      sendingCode: '发送中...',
      codeSent: '已发送',
      codeSentSuccess: '验证码已发送，请使用 {code} 完成注册。',
      submit: '注册',
      submitting: '注册中...',
      success: '注册成功，正在打开管理后台...',
      errors: {
        codeRequest: '验证码发送失败，请检查邮箱后重试。',
        register: '注册失败，请检查验证码后重试。'
      },
      hasAccount: '已经有账号？',
      loginLink: '去登录'
    },
    errors: {
      emailRequired: '请先输入邮箱地址。',
      invalidRegister: '请输入邮箱、密码和验证码。',
      invalidLogin: '请输入邮箱和密码。',
      invalidCode: '验证码错误或已过期。',
      emailExists: '该邮箱已注册。',
      invalidCredentials: '邮箱或密码不正确。',
      unauthorized: '登录状态已过期，请重新登录后继续。',
      registrationDisabled: '当前部署已关闭公开注册，请使用管理员账号登录。',
      network: '网络异常，请检查连接后重试。',
      unknown: '操作失败，请稍后重试。'
    }
  },
  admin: {
    layout: {
      toggleLanguage: '切换语言',
      toggleTheme: '切换主题',
      userProfile: '用户资料',
      switchWorkspace: '切换工作区'
    },
    menu: {
      dashboard: '仪表盘',
      upstream: '上游管理',
      groupRates: '分组倍率',
      groupRateCampaigns: '活动调价',
      settings: '系统设置',
      signOut: '退出登录'
    },
    adminAccounts: {
      title: '选择工作区',
      subtitle: '选择一个管理员工作区以继续，或添加新的工作区。',
      empty: '暂无工作区，添加第一个工作区开始使用。',
      currentLabel: '当前工作区',
      addWorkspace: '添加工作区',
      addWorkspaceHint: '连接新的站点管理员账号',
      creating: '正在创建工作区...',
      errors: {
        noCurrentAccount: '请先选择一个工作区。',
        notFound: '工作区不存在。',
        request: '操作失败，请稍后重试。',
        network: '网络异常，请检查连接后重试。'
      }
    },
    dashboard: {
      metrics: {
        todayProfit: '今日营收',
        siteBalance: '站点用户总余额',
        todayPurchase: '今日成本',
        netProfit: '今日净利润',
        upstreamBalance: '上游总余额',
        groupCount: '我的分组总数',
        groupCountCaption: '点击查看分组详情'
      },
      charts: {
        title: '数据趋势统计',
        subtitle: '查看连续的营收、站点用户余额、成本、净利润与上游总余额走势。',
        trendTitle: '{metric}趋势'
      },
      period: {
        label: '统计周期',
        week: '周',
        month: '月'
      },
      delta: {
        vsPrev: '较前一日'
      },
      loading: '正在加载指标数据...',
      loadError: '加载仪表盘指标失败。',
      retry: '重试',
      loadingModal: {
        title: '正在加载仪表盘数据',
        progress: '加载进度 {progress}%',
        steps: {
          auth: '正在验证管理员身份...',
          data: '正在加载实时指标与历史趋势...',
          done: '正在整理数据并渲染页面...'
        }
      },
      groupList: {
        title: '分组关联',
        subtitle: '共 {count} 组映射',
        close: '关闭',
        empty: '暂无分组映射数据。',
        loadError: '加载分组列表失败。',
        columns: {
          index: '序号',
          ownGroup: '我的分组',
          platform: '平台',
          groupType: '分组类型',
          status: '状态',
          ownMultiplier: '我的倍率',
          upstreamGroup: '对接分组',
          upstreamMultiplier: '对接倍率',
          autoPricing: '自动调价'
        },
        exclusiveLabels: {
          public: '公开',
          exclusive: '专属'
        },
        statusLabels: {
          active: '启用',
          inactive: '禁用'
        },
        autoPricingTip: '开启后，同步倍率时自动在上游倍率基础上加价，支持固定值或百分比两种策略。',
        autoPricingStatus: {
          notConfigured: '未配置',
          enabled: '已开启',
          savedDisabled: '已保存，未启用'
        },
        autoPricingActions: {
          configure: '配置',
          edit: '编辑'
        },
        autoPricingDrawer: {
          title: '自动调价配置',
          titleWithGroup: '{group} · 自动调价配置',
          enableLabel: '启用自动调价',
          sourceLabel: '定价来源',
          sourcePrimaryUpstream: '指定主上游',
          sourceLowestUpstream: '最低倍率上游',
          sourceHighestUpstream: '最高倍率上游',
          sourceAverageUpstream: '平均倍率',
          primaryUpstreamLabel: '主上游',
          primaryUpstreamPlaceholder: '请选择主上游',
          strategyLabel: '加价方式',
          strategyFixed: '固定加价',
          strategyPercentage: '百分比加价',
          fixedIncreaseLabel: '固定加价值',
          percentageIncreaseLabel: '百分比加价值',
          thresholdLabel: '跟随阈值',
          thresholdHelp: '上游变化不超过该百分比时才自动跟随',
          minMultiplierLabel: '最低倍率',
          maxMultiplierLabel: '最高倍率',
          estimatedMultiplier: '预估倍率',
          save: '保存配置',
          cancel: '取消',
          noUpstreams: '当前分组未关联任何上游，无法配置自动调价。',
          noMultiplierData: '暂无可用上游倍率数据，无法计算预估倍率。',
          tips: {
            minMultiplier: '自动计算出的倍率不会低于这个值。用于防止价格过低，保护最低利润。留空表示不限制最低倍率。',
            maxMultiplier: '自动计算出的倍率不会高于这个值。用于防止价格突然过高，影响用户使用。留空表示不限制最高倍率。',
            threshold: '上游倍率变化在该百分比以内时才自动跟随。超过阈值时应等待人工确认，避免上游价格异常波动导致你的分组价格被带偏。',
            minMultiplierAria: '查看最低倍率说明',
            maxMultiplierAria: '查看最高倍率说明',
            thresholdAria: '查看跟随阈值说明',
          },
          guidance: {
            title: '建议设置',
            minMultiplier: '最低倍率：你的成本价 + 最低利润',
            maxMultiplier: '最高倍率：你觉得用户还能接受的最高价',
            threshold: '跟随阈值：10%',
            exampleTitle: '计算示例',
            exampleOld: '上游原倍率：1.00',
            exampleNew: '上游新倍率：1.08',
            exampleThreshold: '跟随阈值：10%',
            exampleMarkup: '加价方式：上游 + 0.10',
            exampleMin: '最低倍率：1.00',
            exampleMax: '最高倍率：1.30',
            exampleResult: '变化幅度为 8%，未超过 10%，因此允许自动跟随；最终倍率为 1.18，并且处于 1.00 到 1.30 的限制范围内。',
          },
          notify: {
            sectionTitle: '自动调价成功通知',
            enableLabel: '调价成功后发送通知',
            enableHelp: '当自动调价实际更新了分组倍率后，通过机器人发送通知。',
            botSelectLabel: '通知机器人',
            botSelectPlaceholder: '选择要通知的机器人',
            noBots: '暂无可用机器人，请先在系统设置的通知与渠道中配置机器人。',
            templateLabel: '通知模板',
            templateHelp: '留空使用默认模板。支持以下变量：',
            templatePlaceholder: '留空则使用默认模板',
            defaultTemplate: '【自动调价】{ownGroup} 已自动从 {oldOwnMultiplier}x 调整为 {newOwnMultiplier}x。参考来源：{upstreamSiteName} / {upstreamGroupName}，参考倍率 {oldReference}x -> {newReference}x。',
            variablesTitle: '可用变量',
            varOwnGroup: '我的分组名',
            varUpstreamSiteName: '上游站点名',
            varUpstreamGroupName: '上游分组名/参考来源',
            varOldReference: '旧参考倍率',
            varNewReference: '新参考倍率',
            varOldOwnMultiplier: '调整前倍率',
            varNewOwnMultiplier: '调整后倍率',
            varStrategy: '加价策略',
            varFixedIncrease: '固定加价值',
            varPercentageIncrease: '百分比加价值',
            varThreshold: '跟随阈值',
            copied: '已复制',
          },
          errors: {
            primaryRequired: '指定主上游模式下必须选择主上游。',
            increaseNonNegative: '加价值不能为负数。',
            thresholdNonNegative: '阈值不能为负数。',
            multiplierNonNegative: '倍率不能为负数。',
            minGreaterThanMax: '最低倍率不能大于最高倍率。',
            invalidConfig: '自动调价配置无效，请检查后重试。',
            notifyBotsRequired: '开启通知时必须至少选择一个机器人。',
          }
        },
        save: '保存',
        saveSuccess: '已保存',
        saving: '保存中...',
        saveError: '保存失败，请重试。'
      },
      groupUsage: {
        title: '今日营收分组明细',
        subtitle: '共 {count} 个分组，合计 {total}',
        close: '关闭',
        empty: '暂无分组用量数据。',
        loadError: '加载分组用量失败。',
        retry: '重试',
        columns: {
          groupName: '分组名称',
          amount: '今日金额'
        },
        sort: {
          desc: '金额从高到低',
          asc: '金额从低到高'
        }
      },
      upstreamKeyUsage: {
        title: '今日成本明细',
        subtitle: '共 {count} 个 key，合计 {total}',
        close: '关闭',
        empty: '暂无今日消费的 key。',
        loadError: '加载今日成本明细失败。',
        retry: '重试',
        columns: {
          siteName: '上游站点',
          keyName: 'Key 名称',
          groupName: '分组',
          amount: '今日金额'
        },
        sort: {
          desc: '金额从高到低',
          asc: '金额从低到高'
        }
      },
      upstreamBalanceBreakdown: {
        title: '上游总余额明细',
        subtitle: '共 {count} 个站点，合计 {total}',
        close: '关闭',
        empty: '暂无上游站点余额数据。',
        loadError: '加载上游余额明细失败。',
        retry: '重试',
        unknownBalance: '未知余额',
        neverSynced: '尚未同步',
        columns: {
          siteName: '上游站点',
          status: '状态',
          lastSyncedAt: '最近同步时间',
          balance: '余额'
        },
        sort: {
          desc: '余额从高到低',
          asc: '余额从低到高'
        }
      },
      balanceFilter: {
        title: '余额筛选条件',
        subtitle: '配置统计站点用户总余额时的过滤规则。',
        close: '关闭',
        excludeAdmin: '排除管理员账户',
        excludeAdminHelp: '统计时不包含 admin 角色用户的余额。',
        excludeBalances: '排除特定余额值',
        excludeBalancesHelp: '余额等于以下数值的用户将不纳入统计。',
        addPlaceholder: '输入要排除的余额值',
        add: '添加',
        cancel: '取消',
        save: '保存',
        saving: '保存中...',
        loadError: '加载筛选配置失败。',
        saveError: '保存筛选配置失败。'
      },
      adminAuth: {
        loggedInAs: '当前 admin：{identity}',
        logout: '退出当前 admin 账户',
        notLoggedIn: '尚未登录 admin 账户',
        login: '登录 admin 账户',
        expiresAt: '过期',
        timeUnknown: '未知',
        logoutConfirm: {
          title: '退出当前 admin 账户？',
          description: '退出后需要重新登录并校验 admin 身份才能查看仪表盘数据。',
          confirm: '确认退出',
          cancel: '取消'
        },
        dataLocked: {
          title: '请先登录 admin 账户',
          description: '登录并校验通过具备 admin 权限的站点账号后，才能查看仪表盘统计数据。'
        },
        modal: {
          title: '登录 admin 账户',
          subtitle: '仪表盘需要一个具备 admin 权限的站点账户。',
          close: '关闭',
          platformLabel: '选择平台',
          platform: {
            sub2api: 'Sub2API',
            newapi: 'New-API'
          },
          comingSoon: '即将支持',
          newApiPasswordOnly: 'New-API 仅支持账号密码登录。',
          siteUrlLabel: '站点地址（域名或 IP）',
          siteUrlPlaceholder: '如 https://sub.example.com 或 http://1.2.3.4:5555',
          methodLabel: '登录方式',
          method: {
            password: '邮箱密码',
            token: 'RT + AT'
          },
          fields: {
            emailPlaceholder: '管理员邮箱',
            usernamePlaceholder: '管理员账号',
            passwordPlaceholder: '管理员密码',
            accessTokenPlaceholder: 'Access Token（可选，可留空）',
            refreshTokenPlaceholder: 'Refresh Token（必填）',
            tokenTypePlaceholder: 'Token Type（可选，默认 Bearer）',
            tokenHelp: '只需填写 Refresh Token 即可登录：系统会先用它刷新一次以获取最新过期时间，并在临期时自动刷新。'
          },
          submit: '登录并校验',
          submitting: '校验中...'
        },
        errors: {
          request: 'admin 登录请求失败，请稍后重试。',
          missingCredentials: '请填写站点地址和所选登录方式的必填项。',
          invalidUrl: '站点地址无效，请填写正确的域名或 IP 后重试。',
          adminOnly: '该账户不是 admin 或鉴权失败，请确认凭证后重试。',
          network: '网络或跨域请求失败，请检查站点地址。',
          platformUnsupported: '不支持的平台类型，请选择 Sub2API 或 New-API。',
          unknown: 'admin 登录时发生未知错误。'
        }
      }
    },
      upstream: {
        searchPlaceholder: '搜索站点名称...',
        addSite: '新增站点',
        summary: '已连接 {connected} / {total} 个上游站点',
        refresh: {
          action: '刷新数据',
          refreshing: '刷新中...',
          countdown: '{seconds} 秒后刷新',
          disabled: '未开启自动刷新'
        },
      modal: {
        title: '新增上游站点',
        editTitle: '修改上游站点',
        cancel: '取消',
        submit: '确认新增',
        updateSubmit: '保存修改',
        submitting: '连接中...',
        form: {
          siteName: '站点名称',
          siteNamePlaceholder: '输入站点名称',
          siteUrl: '站点 URL',
          siteUrlPlaceholder: '输入完整的站点地址，如 https://api.example.com',
          platform: '选择平台',
          platforms: {
            auto: '自动识别',
            sub2api: 'Sub2API',
            newapi: 'New-API'
          },
          authMode: '认证方式',
          authModes: {
            password: '账号密码登录',
            passwordHelp: '使用站点账号密码登录，并自动保存会话。',
            token: 'Access Token / Refresh Token',
            tokenHelp: '适用于 Cloudflare 或二次验证导致账号密码无法直连的 Sub2API 站点。'
          },
          account: '登录账号',
          accountPlaceholder: '输入账号',
          password: '登录密码',
          passwordPlaceholder: '输入密码',
          passwordEditPlaceholder: '不修改密码请留空',
          passwordEditHelp: '留空时不会重新登录，也不会修改已保存的登录会话；填写新密码后才会重新登录并更新会话。',
          accessToken: 'Access Token',
          accessTokenPlaceholder: '粘贴 access_token，可留空并仅提供 refresh_token',
          refreshToken: 'Refresh Token',
          refreshTokenPlaceholder: '粘贴 refresh_token，系统会先刷新一次获取最新过期时间',
          tokenType: 'Token Type',
          tokenTypePlaceholder: '默认 Bearer',
          tokenHelp: '如果提供 refresh_token，系统会优先调用刷新接口换取新的 access_token 和过期时间。',
          rechargeRate: '充值倍率',
          rechargeRatePlaceholder: '输入 USD 转 CNY 的倍率，如 7',
          rechargeRateHelp: '必填。人民币金额 = USD 金额 × 此倍率。',
          remark: '备注',
          remarkPlaceholder: '输入备注信息（可选）'
        }
      },
      currency: {
        usdValue: '{amount} USD',
        cnyValue: '{amount} CNY'
      },
      fields: {
        siteName: '站名',
        siteUrl: '站点 URL',
        platform: '平台',
        balance: '余额',
        todayConsume: '今日消费',
        historyRecharge: '历史充值',
        groupName: '分组名称',
        groupMultiplier: '分组倍率',
        availableGroups: '可用分组',
        viewAvailableGroups: '查看可用分组',
        closeGroupsModal: '关闭',
        unknownPlatform: '未知类型',
        isConnected: '是否对接',
        connected: '已对接',
        disconnected: '未对接',
        lastUpdated: '更新时间',
        notSynced: '暂未同步'
      },
      status: {
        connecting: '连接中',
        syncing: '同步中',
        connected: '已连接',
        error: '异常'
      },
      empty: {
        title: '未找到上游站点',
        description: '请调整搜索条件，或新增一个上游站点。'
      },
      delete: {
        action: '删除站点',
        title: '确认删除上游站点？',
        description: '你将删除“{name}”，删除后需要重新添加和登录才能恢复。',
        cancel: '取消',
        confirm: '确认删除'
      },
      action: {
        sync: '刷新',
        syncing: '刷新中',
        edit: '修改站点',
        settings: '站点设置',
        actions: '操作'
      },
      siteSettings: {
        title: '站点预警设置',
        balanceThreshold: '自定义余额预警阈值',
        balanceThresholdHelp: '开启后使用站点专属阈值，关闭则使用全局默认值。',
        balanceThresholdPlaceholder: '输入阈值金额',
        save: '保存',
        saveSuccess: '已保存',
        saving: '保存中...',
        cancel: '取消'
      },
      viewMode: {
        list: '列表模式',
        card: '卡片模式'
      },
      syncStream: {
        syncing: '正在同步...',
        done: '同步完成',
        error: '同步失败',
      },
      errors: {
        invalidUrl: '站点 URL 无效，请检查后重试。',
        network: '网络或 CORS 请求失败，请检查站点地址与跨域配置。',
        auth: '登录失败，请检查账号或密码。',
        request: '上游接口请求失败，请稍后重试。',
        invalidResponse: '上游返回内容无法解析。',
        tokenMissing: '登录成功但未返回访问令牌。',
        detect: '无法自动识别平台，请手动选择平台后重试。',
        unknown: '连接上游站点时发生未知错误。'
      }
    },
    groupRates: {
      badge: '倍率同步记录',
      title: '分组倍率',
      subtitle: '查看各上游站点分组倍率及最近变动，并追踪历史倍率记录。',
      common: {
        placeholder: '—',
        allTypes: '全部类型',
        allPlatforms: '全部平台',
        unknown: '未知'
      },
      platforms: {
        newapi: 'New-API',
        sub2api: 'Sub2API'
      },
      summary: {
        totalLabel: '分组倍率总数',
        updatedLabel: '已同步记录'
      },
      table: {
        title: '倍率列表',
        description: '列表顺序与后端返回保持一致。'
      },
      fields: {
        siteName: '站点名称',
        groupName: '分组名称',
        type: '分组类型',
        platform: '站点平台',
        currentMultiplier: '当前倍率',
        delta: '涨跌幅',
        updatedAt: '更新时间',
        actions: '操作'
      },
      actions: {
        refresh: '刷新数据',
        createCampaign: '创建活动',
        viewHistory: '查看历史',
        viewHistoryForRate: '查看 {site} · {group} 的涨跌幅历史，当前涨跌幅 {delta}',
        closeHistory: '关闭历史',
        editType: '修改',
        closeEdit: '关闭修改分组类型',
        connect: '点击对接',
        closeConnect: '关闭对接窗口',
        saveConnect: '确认对接',
        cancel: '取消',
        saveType: '保存类型'
      },
      filters: {
        searchLabel: '搜索',
        searchPlaceholder: '搜索站点或分组...',
        typeLabel: '分组类型',
        platformLabel: '站点平台'
      },
      sort: {
        label: '排序',
        multiplierAsc: '倍率从低到高',
        multiplierDesc: '倍率从高到低',
        siteNameAsc: '站点名称 A-Z',
        groupNameAsc: '分组名称 A-Z'
      },
      tabs: {
        all: '全部',
        mapped: '已对接',
        unmapped: '未对接',
        deleted: '已删除'
      },
      pagination: {
        previous: '上一页',
        next: '下一页',
        currentPage: '第 {page} / {totalPages} 页',
        total: '共 {total} 条',
        pageSize: '每页 {pageSize} 条'
      },
      status: {
        loading: '正在加载分组倍率...',
        mapped: '已对接',
        unmapped: '未对接',
        deleted: '已删除'
      },
      empty: {
        title: '暂无分组倍率',
        description: '同步上游站点后，这里会显示分组倍率数据。'
      },
      history: {
        title: '倍率历史',
        titleWithGroup: '{site} · {group} 倍率历史',
        subtitle: '平台：{platform}',
        loading: '正在加载历史记录...',
        emptyTitle: '暂无历史记录',
        emptyDescription: '该站点分组暂未返回倍率历史。',
        multiplier: '倍率',
        delta: '涨跌幅',
        createdAt: '记录时间'
      },
      edit: {
        title: '修改分组类型',
        titleWithGroup: '修改 {site} · {group} 的分组类型',
        description: '保存后会更新该站点分组的倍率类型，并刷新列表。',
        typeLabel: '分组类型',
        typePlaceholder: '请选择分组类型'
      },
      connect: {
        titleWithGroup: '对接 {site} · {group}',
        description: '选择我的站点分组后，会把当前上游分组加入该分组的对接关系。',
        ownGroupLabel: '我的站点分组',
        ownGroupPlaceholder: '请选择我的站点分组',
        upstreamGroupLabel: '对接分组',
        upstreamGroupPlaceholder: '请选择对接分组',
        upstreamSiteLabel: '上游站点',
        upstreamGroupNameLabel: '上游分组',
        upstreamMultiplierLabel: '上游倍率',
        upstreamPlatformLabel: '平台',
        modeData: '数据统计',
        modeReal: '真实对接',
        realDescription: '将自动在上游站点创建 API Key，并在 admin 站点创建对应的转发账号。',
        groupTypeLabel: '分组类型',
        groupTypePlaceholder: '请选择分组类型',
        groupTypeOpenai: 'OpenAI',
        groupTypeAnthropic: 'Anthropic',
        groupTypeGemini: 'Gemini',
        groupTypeAntigravity: 'Antigravity',
        channelTypeLabel: '渠道类型',
        channelTypePlaceholder: '请选择渠道类型',
        realNotSupported: '当前平台不支持真实对接',
        realConnecting: '正在创建对接...',
        realSuccess: '真实对接创建成功',
        realFailed: '真实对接创建失败',
        modeBind: '手动绑定',
        bindDescription: '选择已有的上游 Key 绑定到当前分组，不会创建新资源。',
        bindSelectKey: '选择上游 Key',
        bindKeysLoading: '正在加载 Key 列表...',
        bindKeysEmpty: '该站点暂无可用 Key',
        bindFailed: '手动绑定失败'
      },
      disconnect: {
        action: '取消对接',
        title: '取消对接',
        description: '确认取消 {site} · {group} 的真实对接？',
        unlinkOnly: '仅取消关联',
        unlinkOnlyHint: '仅删除本地绑定记录，保留上游 Key 和 Admin 账号',
        deleteAll: '删除账号和 Key',
        deleteAllHint: '同时删除上游 Key 和 Admin 站点的转发账号',
        confirm: '确定',
        disconnecting: '正在取消对接...',
        failed: '取消对接失败'
      },
      format: {
        multiplier: '{value}x',
        deltaMultiplier: '{value}x'
      },
      errors: {
        network: '网络或 CORS 请求失败，请检查接口地址与跨域配置。',
        request: '分组倍率接口请求失败，请稍后重试。',
        unknown: '加载分组倍率时发生未知错误。'
      }
    },
    groupRateCampaigns: {
      title: '活动调价',
      subtitle: '批量调整自有分组倍率，支持定时开始/结束并自动恢复原倍率。',
      common: {
        placeholder: '—'
      },
      actions: {
        create: '新建活动',
        refresh: '刷新',
        start: '立即开始',
        end: '结束活动',
        cancel: '取消活动',
        viewDetail: '查看详情',
        close: '关闭',
        preview: '预览影响',
        confirmCreate: '创建活动',
        cancelEdit: '取消'
      },
      tabs: {
        all: '全部'
      },
      status: {
        draft: '草稿',
        scheduled: '待开始',
        running: '进行中',
        ending: '结束中',
        ended: '已结束',
        partial: '部分成功',
        failed: '失败',
        cancelled: '已取消',
        loading: '正在加载活动...'
      },
      fields: {
        name: '活动名称',
        status: '状态',
        startAt: '开始时间',
        endAt: '结束时间',
        summary: '执行结果',
        createdBy: '创建人',
        actions: '操作'
      },
      empty: {
        title: '暂无活动',
        description: '点击"新建活动"创建第一个批量调价活动。'
      },
      pagination: {
        total: '共 {total} 个',
        pageSize: '每页 {pageSize} 条',
        currentPage: '第 {page} / {totalPages} 页',
        previous: '上一页',
        next: '下一页'
      },
      format: {
        summary: '{applied}/{total} 已生效'
      },
      errors: {
        network: '网络或 CORS 请求失败，请检查接口地址与跨域配置。',
        request: '活动调价接口请求失败，请稍后重试。',
        unknown: '加载活动调价时发生未知错误。',
        emptySelection: '请至少选择一个分组，且分组必须存在于自有分组中。',
        invalidName: '活动名称无效，请检查长度是否在 1-80 个字符之间。',
        invalidAdjustment: '活动倍率无效，请检查每个分组是否填写了有效的固定倍率。',
        invalidSchedule: '时间计划无效，请检查开始/结束时间设置。',
        noNotifyBots: '开启通知后请至少选择一个机器人。',
        notFound: '活动不存在。',
        invalidState: '当前活动状态不支持该操作。',
        duplicateGroup: '同一个分组不能重复选择。'
      },
      editor: {
        titleCreate: '新建活动调价',
        sectionInfo: '活动信息',
        nameLabel: '活动名称',
        namePlaceholder: '例如：双十一活动',
        descriptionLabel: '活动描述',
        descriptionPlaceholder: '选填，方便自己识别活动用途',
        sectionSelection: '选择分组',
        selectionHint: '每个分组单独设置活动倍率',
        groupsEmpty: '暂无可选分组',
        groupMultiplierPlaceholder: '活动倍率',
        sectionSchedule: '时间计划',
        startModeLabel: '开始方式',
        startNow: '立即开始',
        startScheduled: '定时开始',
        startDraft: '保存为草稿',
        startAtLabel: '开始时间',
        endModeLabel: '结束方式',
        endScheduled: '定时结束',
        endManual: '手动结束',
        endAtLabel: '结束时间',
        sectionNotify: '通知',
        notifyEnableLabel: '开启机器人通知',
        notifyBotSelectLabel: '选择机器人',
        notifyNoBots: '暂无可用机器人，请先在系统设置中配置。',
        notifyStartTemplateLabel: '开始通知文案',
        notifyEndTemplateLabel: '结束通知文案',
        notifyVariablesTitle: '可用变量，点击复制',
        notifyVarActivityName: '活动名称',
        notifyVarTotalCount: '目标分组总数',
        notifyVarAppliedCount: '已生效数量',
        notifyVarFailedCount: '失败数量',
        notifyVarStartTime: '开始时间',
        notifyVarEndTime: '结束时间',
        copyVarFailed: '复制失败，请手动复制变量。',
        previewTitle: '预览影响的分组',
        previewEmpty: '暂无预览结果，点击"预览影响"查看',
        previewGroupName: '分组名称',
        previewOriginal: '原倍率',
        previewCampaign: '活动倍率',
        previewTotal: '共 {total} 个分组受影响',
        errors: {
          nameRequired: '请输入活动名称',
          selectionEmpty: '请至少选择一个分组',
          groupMultiplierInvalid: '请为每个分组填写有效活动倍率',
          scheduleInvalid: '请检查开始/结束时间设置',
          notifyBotsRequired: '开启通知后请至少选择一个机器人'
        }
      },
      detail: {
        title: '活动详情',
        sectionConfig: '活动配置',
        sectionItems: '分组明细',
        itemGroupName: '分组名称',
        itemOriginal: '原倍率',
        itemCampaign: '活动倍率',
        itemRestored: '恢复倍率',
        itemApplyStatus: '开始状态',
        itemRestoreStatus: '恢复状态',
        noReason: '—',
        confirmEnd: '确定要手动结束该活动吗？将立即尝试恢复所有分组的原倍率。',
        confirmCancel: '确定要取消该活动吗？取消后不会执行任何调价操作。'
      }
    },
    mySites: {
      errors: {
        invalidAutoPricingConfig: '自动调价配置无效：主上游不在关联上游中，或最低倍率大于最高倍率。'
      }
    },
    settings: {
      title: '系统设置',
      subtitle: '管理系统运行参数、通知渠道及自动化策略。',
      save: '保存配置',
      saving: '保存中...',
      saveSuccess: '已保存',
      strategyDescription: '配置数据刷新频率、预警阈值和自动化策略。',
      requiresRefresh: '建议先开启数据刷新频率，以便系统自动检测变化并触发预警。',
      balanceWarningAmount: '触发金额（CNY）',
      notifyBots: '发送通知到机器人',
      customTemplate: '自定义通知文案',
      balanceTemplateVars: '(支持变量: {siteName}, {balance}, {threshold})',
      multiplierTemplateVars: '(支持变量: {siteName}, {groupName}, {oldRate}, {newRate}, {changeDirection})',
      unnamedBot: '未命名机器人',
      noBotsConfigured: '请先在"通知与渠道"中配置机器人',
      mustSelectBot: '必须选择至少一个通知机器人',
      varSiteName: '站点名称',
      varBalance: '当前余额（CNY）',
      varThreshold: '阈值金额（CNY）',
      varGroupName: '分组名称',
      varOldRate: '原倍率',
      varNewRate: '新倍率',
      varChangeDirection: '变更方向',
      pricingAmount: '调价幅度',
      botNameLabel: '机器人名称标识',
      botNameDingtalkPlaceholder: '例如：钉钉主群',
      botNameFeishuPlaceholder: '例如：飞书主群',
      botNameTelegramPlaceholder: '例如：TG主群',
      addDingtalkBot: '添加钉钉机器人',
      addFeishuBot: '添加飞书机器人',
      addTelegramBot: '添加 TG 机器人',
      emptyDingtalk: '暂无钉钉机器人配置',
      emptyFeishu: '暂无飞书机器人配置',
      emptyTelegram: '暂无 Telegram 机器人配置',
      tabs: {
        strategy: '自动化与策略',
        channels: '通知与渠道',
        templates: '消息模板'
      },
      sections: {
        basic: {
          title: '基础设置',
          description: '配置系统的基础运行参数。',
          refreshInterval: '数据刷新频率',
          refreshIntervalHelp: '设置系统在后台自动拉取并刷新上游站点数据的时间间隔，最低 60 秒。',
          seconds: '秒'
        },
        thresholds: {
          title: '站点预警阈值',
          description: '配置针对所有上游站点的默认报警触发条件。',
          balanceWarning: '余额预警',
          balanceWarningHelp: '当某个上游站点的余额（按充值倍率折合人民币）低于设定金额时，通过机器人发送预警通知。',
          multiplierChangeWarning: '倍率变更预警',
          multiplierChangeWarningHelp: '当监控的对接分组倍率发生任何变动时，通过机器人发送通知。'
        },
        pricing: {
          title: '分组倍率调价',
          description: '配置对接后的某个分组在倍率上涨时的自动处理策略。',
          enableAutoPricing: '自动调价',
          enableAutoPricingHelp: '当对接的上游分组倍率上涨时，自动调整"我的分组"的倍率。',
          strategy: '调价策略',
          strategyFixed: '固定涨幅 (+)',
          strategyPercentage: '百分比涨幅 (%)',
          fixedValuePlaceholder: '例如 0.1',
          percentageValuePlaceholder: '例如 10'
        },
        channels: {
          title: '通知渠道配置',
          description: '配置接收系统报警的第三方渠道参数（如钉钉、TG、飞书）。',
          dingtalk: '钉钉机器人',
          dingtalkHelp: '配置钉钉群机器人的 Webhook 和加签密钥。',
          feishu: '飞书机器人',
          feishuHelp: '配置飞书群机器人的 Webhook 和加签密钥。',
          telegram: 'Telegram 机器人',
          telegramHelp: '配置 Telegram Bot Token 和接收消息的 Chat ID。',
          webhookUrl: 'Webhook 地址',
          secret: '加签密钥 (Secret)',
          botToken: 'Bot Token',
          chatId: 'Chat ID',
          proxyUrl: '代理地址（可选）',
          proxyUrlPlaceholder: '例如 http://127.0.0.1:7890',
          proxyUrlHelp: '服务器无法直连 Telegram 时填写代理地址；留空则直连。',
          loading: '正在加载通知渠道配置...',
          testConnection: '测试连通性',
          testConnectionSuccess: '发送成功'
        },
        templates: {
          balanceTemplatePlaceholder: '例如：【余额预警】{siteName} 站点余额（CNY）已不足 {threshold} 元，当前余额为 {balance} 元。',
          multiplierTemplatePlaceholder: '例如：【倍率变更】{siteName} 的 {groupName} 分组倍率已从 {oldRate}x 变为 {newRate}x。'
        }
      },
      errors: {
        network: '网络或 CORS 请求失败，请检查接口地址与跨域配置。',
        request: '通知渠道测试请求失败，请稍后重试。',
        unknown: '测试通知渠道时发生未知错误。',
        invalidChannel: '通知渠道类型无效。',
        missingWebhook: '请先填写机器人 Webhook 地址。',
        missingTelegramConfig: '请先填写 Telegram Bot Token 和 Chat ID。',
        sendFailed: '测试消息发送失败，请检查机器人配置和网络连通性。'
      }
    },
    system: {
      version: '版本 {version}',
      errors: {
        network: '系统信息请求失败，请检查网络连接。',
        request: '系统请求失败，请稍后重试。'
      }
    }
  }
}
