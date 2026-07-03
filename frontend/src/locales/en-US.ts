export default {
  nav: {
    features: 'Features',
    integrations: 'Integrations',
    documentation: 'Documentation',
    pricing: 'Pricing',
    signIn: 'Sign In',
    getStarted: 'Get Started'
  },
  hero: {
    badge: 'Introducing TransitHub 2.0',
    title: 'The Ultimate',
    highlight: 'API Gateway',
    subtitle: 'Unify your NewAPI instances, manage keys with ease, and route traffic intelligently. Built for the modern AI infrastructure.',
    startBtn: 'Start Building Now',
    docBtn: 'View Documentation'
  },
  features: {
    title: 'Designed for Scale and Speed',
    subtitle: 'Everything you need to manage massive API traffic across distributed networks, packed into one beautiful interface.',
    items: {
      sync: {
        title: 'Multi-Instance Sync',
        desc: 'Seamlessly synchronize across multiple NewAPI instances with zero downtime and automatic conflict resolution.'
      },
      fallback: {
        title: 'Smart Fallback',
        desc: 'Intelligent routing and automatic fallback ensures your API requests never fail even if a provider goes down.'
      },
      observe: {
        title: 'Global Observability',
        desc: 'Monitor all your API keys, quotas, and latency metrics in real-time across the globe.'
      },
      selfhost: {
        title: 'Self-Hosted Ready',
        desc: 'Deploy anywhere. Full support for Docker, Kubernetes, and bare-metal VPS installations.'
      }
    }
  },
  cta: {
    title: 'Ready to take control?',
    subtitle: 'Join thousands of developers using TransitHub to power their API infrastructure. Get started for free today.',
    deployBtn: 'Deploy Now',
    salesBtn: 'Contact Sales'
  },
  footer: {
    rights: 'TransitHub Operations. All rights reserved.'
  },
  auth: {
    backToHome: 'Back to Home',
    login: {
      title: 'Welcome Back',
      subtitle: 'Enter your email and password to sign in',
      email: 'Email',
      emailPlaceholder: "name{'@'}example.com",
      password: 'Password',
      passwordPlaceholder: 'Enter your password',
      submit: 'Sign In',
      submitting: 'Signing in...',
      success: 'Signed in successfully. Opening the admin console...',
      errors: {
        login: 'Unable to sign in. Check your email and password, then try again.'
      },
      noAccount: 'Don\'t have an account?',
      registerLink: 'Register now'
    },
    register: {
      title: 'Create an Account',
      subtitle: 'Enter your details to register for TransitHub',
      email: 'Email',
      emailPlaceholder: "name{'@'}example.com",
      password: 'Password',
      passwordPlaceholder: 'Set a password',
      code: 'Verification Code',
      codePlaceholder: 'Enter 6-digit code',
      sendCode: 'Send Code',
      sendingCode: 'Sending...',
      codeSent: 'Sent',
      codeSentSuccess: 'Verification code sent. Use {code} to finish registration.',
      submit: 'Register',
      submitting: 'Registering...',
      success: 'Registration complete. Opening the admin console...',
      errors: {
        codeRequest: 'Unable to send the verification code. Check your email and try again.',
        register: 'Unable to register. Check the verification code and try again.'
      },
      hasAccount: 'Already have an account?',
      loginLink: 'Sign In'
    },
    errors: {
      emailRequired: 'Enter an email address first.',
      invalidRegister: 'Enter your email, password, and verification code.',
      invalidLogin: 'Enter your email and password.',
      invalidCode: 'The verification code is incorrect or expired.',
      emailExists: 'This email is already registered.',
      invalidCredentials: 'Email or password is incorrect.',
      unauthorized: 'Your session has expired. Please sign in again to continue.',
      registrationDisabled: 'Public registration is disabled for this deployment. Sign in with the admin account.',
      network: 'Network error. Check your connection and try again.',
      unknown: 'Something went wrong. Please try again.'
    }
  },
  admin: {
    layout: {
      toggleLanguage: 'Toggle language',
      toggleTheme: 'Toggle theme',
      userProfile: 'User profile',
      switchWorkspace: 'Switch Workspace'
    },
    menu: {
      dashboard: 'Dashboard',
      upstream: 'Upstream',
      groupRates: 'Group Rates',
      groupRateCampaigns: 'Rate Campaigns',
      settings: 'Settings',
      signOut: 'Sign Out'
    },
    adminAccounts: {
      title: 'Select Workspace',
      subtitle: 'Choose an admin workspace to continue, or add a new one.',
      empty: 'No workspaces yet. Add your first workspace to get started.',
      currentLabel: 'Current workspace',
      addWorkspace: 'Add Workspace',
      addWorkspaceHint: 'Connect a new site admin account',
      creating: 'Creating workspace...',
      errors: {
        noCurrentAccount: 'Please select a workspace first.',
        notFound: 'Workspace not found.',
        request: 'Operation failed. Please try again.',
        network: 'Network error. Check your connection and try again.'
      }
    },
    dashboard: {
      metrics: {
        todayProfit: "Today's Revenue",
        siteBalance: 'Site User Balance',
        todayPurchase: "Today's Cost",
        netProfit: "Today's Net Profit",
        upstreamBalance: 'Upstream Total Balance',
        groupCount: 'My Groups',
        groupCountCaption: 'Click to view group details'
      },
      charts: {
        title: 'Trend Analytics',
        subtitle: 'Track continuous revenue, site user balance, cost, net profit and upstream balance over time.',
        trendTitle: '{metric} Trend'
      },
      period: {
        label: 'Period',
        week: 'Week',
        month: 'Month'
      },
      delta: {
        vsPrev: 'vs prev day'
      },
      loading: 'Loading metrics...',
      loadError: 'Failed to load dashboard metrics.',
      retry: 'Retry',
      loadingModal: {
        title: 'Loading Dashboard Data',
        progress: '{progress}% complete',
        steps: {
          auth: 'Verifying admin credentials...',
          data: 'Loading metrics and trends...',
          done: 'Preparing data and rendering page...'
        }
      },
      groupList: {
        title: 'Group Mappings',
        subtitle: '{count} mappings total',
        close: 'Close',
        empty: 'No group mappings found.',
        loadError: 'Failed to load group list.',
        columns: {
          index: '#',
          ownGroup: 'Own Group',
          platform: 'Platform',
          groupType: 'Group Type',
          status: 'Status',
          ownMultiplier: 'Own Multiplier',
          upstreamGroup: 'Upstream Group',
          upstreamMultiplier: 'Upstream Multiplier',
          autoPricing: 'Auto Pricing'
        },
        exclusiveLabels: {
          public: 'Public',
          exclusive: 'Exclusive'
        },
        statusLabels: {
          active: 'Active',
          inactive: 'Inactive'
        },
        autoPricingTip: 'When enabled, automatically adds a markup on top of the upstream multiplier during sync. Supports fixed value or percentage strategies.',
        autoPricingStatus: {
          notConfigured: 'Not configured',
          enabled: 'Enabled',
          savedDisabled: 'Saved, not enabled'
        },
        autoPricingActions: {
          configure: 'Configure',
          edit: 'Edit'
        },
        autoPricingDrawer: {
          title: 'Auto-Pricing Config',
          titleWithGroup: '{group} · Auto-Pricing Config',
          enableLabel: 'Enable Auto-Pricing',
          sourceLabel: 'Pricing Source',
          sourcePrimaryUpstream: 'Primary Upstream',
          sourceLowestUpstream: 'Lowest Upstream',
          sourceHighestUpstream: 'Highest Upstream',
          sourceAverageUpstream: 'Average Upstream',
          primaryUpstreamLabel: 'Primary Upstream',
          primaryUpstreamPlaceholder: 'Select primary upstream',
          strategyLabel: 'Markup Method',
          strategyFixed: 'Fixed Increase',
          strategyPercentage: 'Percentage Increase',
          fixedIncreaseLabel: 'Fixed Increase Value',
          percentageIncreaseLabel: 'Percentage Increase Value',
          thresholdLabel: 'Follow Threshold',
          thresholdHelp: 'Auto-follow only when upstream change is within this percentage',
          minMultiplierLabel: 'Min Multiplier',
          maxMultiplierLabel: 'Max Multiplier',
          estimatedMultiplier: 'Estimated Multiplier',
          save: 'Save Config',
          cancel: 'Cancel',
          noUpstreams: 'No upstreams linked to this group. Cannot configure auto-pricing.',
          noMultiplierData: 'No upstream multiplier data available. Cannot compute estimated multiplier.',
          tips: {
            minMultiplier: 'The calculated multiplier will not go below this value. Use it to protect your minimum margin. Leave empty for no lower limit.',
            maxMultiplier: 'The calculated multiplier will not go above this value. Use it to avoid sudden price spikes for users. Leave empty for no upper limit.',
            threshold: 'Auto-follow only when the upstream multiplier changes within this percentage. Larger changes should wait for manual confirmation to avoid abnormal upstream swings changing your group price.',
            minMultiplierAria: 'View min multiplier help',
            maxMultiplierAria: 'View max multiplier help',
            thresholdAria: 'View follow threshold help',
          },
          guidance: {
            title: 'Recommended Settings',
            minMultiplier: 'Min Multiplier: your cost + minimum profit margin',
            maxMultiplier: 'Max Multiplier: the highest price users would still accept',
            threshold: 'Follow Threshold: 10%',
            exampleTitle: 'Calculation Example',
            exampleOld: 'Upstream old multiplier: 1.00',
            exampleNew: 'Upstream new multiplier: 1.08',
            exampleThreshold: 'Follow threshold: 10%',
            exampleMarkup: 'Markup method: upstream + 0.10',
            exampleMin: 'Min multiplier: 1.00',
            exampleMax: 'Max multiplier: 1.30',
            exampleResult: 'The change is 8%, within the 10% threshold, so auto-follow is allowed. The final multiplier is 1.18, which falls within the 1.00–1.30 limit range.',
          },
          notify: {
            sectionTitle: 'Auto-Pricing Success Notification',
            enableLabel: 'Send notification after pricing update',
            enableHelp: 'Send a bot notification when auto-pricing actually updates the group multiplier.',
            botSelectLabel: 'Notification Bots',
            botSelectPlaceholder: 'Select bots to notify',
            noBots: 'No bots available. Please configure bots in System Settings > Notifications & Channels first.',
            templateLabel: 'Notification Template',
            templateHelp: 'Leave empty to use the default template. Supported variables:',
            templatePlaceholder: 'Leave empty to use default template',
            defaultTemplate: '[Auto Pricing] {ownGroup} was adjusted from {oldOwnMultiplier}x to {newOwnMultiplier}x. Reference: {upstreamSiteName} / {upstreamGroupName}, multiplier {oldReference}x -> {newReference}x.',
            variablesTitle: 'Available Variables',
            varOwnGroup: 'Own group name',
            varUpstreamSiteName: 'Upstream site name',
            varUpstreamGroupName: 'Upstream group name / source',
            varOldReference: 'Old reference multiplier',
            varNewReference: 'New reference multiplier',
            varOldOwnMultiplier: 'Multiplier before adjustment',
            varNewOwnMultiplier: 'Multiplier after adjustment',
            varStrategy: 'Pricing strategy',
            varFixedIncrease: 'Fixed increase value',
            varPercentageIncrease: 'Percentage increase value',
            varThreshold: 'Follow threshold',
            copied: 'Copied',
          },
          errors: {
            primaryRequired: 'Primary upstream must be selected in primary upstream mode.',
            increaseNonNegative: 'Increase value cannot be negative.',
            thresholdNonNegative: 'Threshold cannot be negative.',
            multiplierNonNegative: 'Multiplier cannot be negative.',
            minGreaterThanMax: 'Min multiplier cannot be greater than max multiplier.',
            invalidConfig: 'Invalid auto-pricing config. Please check and try again.',
            notifyBotsRequired: 'At least one bot must be selected when notifications are enabled.',
          }
        },
        save: 'Save',
        saveSuccess: 'Saved',
        saving: 'Saving...',
        saveError: 'Save failed. Please try again.'
      },
      groupUsage: {
        title: 'Today\'s Revenue by Group',
        subtitle: '{count} groups, {total} total',
        close: 'Close',
        empty: 'No group usage data available.',
        loadError: 'Failed to load group usage data.',
        retry: 'Retry',
        columns: {
          groupName: 'Group Name',
          amount: 'Today\'s Amount'
        },
        sort: {
          desc: 'Amount: High to Low',
          asc: 'Amount: Low to High'
        }
      },
      upstreamKeyUsage: {
        title: 'Today\'s Cost Breakdown',
        subtitle: '{count} keys, {total} total',
        close: 'Close',
        empty: 'No keys with usage today.',
        loadError: 'Failed to load today\'s cost breakdown.',
        retry: 'Retry',
        columns: {
          siteName: 'Upstream Site',
          keyName: 'Key Name',
          groupName: 'Group',
          amount: 'Today\'s Amount'
        },
        sort: {
          desc: 'Amount: High to Low',
          asc: 'Amount: Low to High'
        }
      },
      upstreamBalanceBreakdown: {
        title: 'Upstream Balance Breakdown',
        subtitle: '{count} sites, {total} total',
        close: 'Close',
        empty: 'No upstream site balance data available.',
        loadError: 'Failed to load upstream balance breakdown.',
        retry: 'Retry',
        unknownBalance: 'Unknown',
        neverSynced: 'Never synced',
        columns: {
          siteName: 'Upstream Site',
          status: 'Status',
          lastSyncedAt: 'Last Synced',
          balance: 'Balance'
        },
        sort: {
          desc: 'Balance: High to Low',
          asc: 'Balance: Low to High'
        }
      },
      balanceFilter: {
        title: 'Balance Filter',
        subtitle: 'Configure filtering rules for site user balance calculation.',
        close: 'Close',
        excludeAdmin: 'Exclude admin accounts',
        excludeAdminHelp: 'Do not include admin role users in the balance total.',
        excludeBalances: 'Exclude specific balance values',
        excludeBalancesHelp: 'Users whose balance equals any of these values will be excluded.',
        addPlaceholder: 'Enter a balance value to exclude',
        add: 'Add',
        cancel: 'Cancel',
        save: 'Save',
        saving: 'Saving...',
        loadError: 'Failed to load filter config.',
        saveError: 'Failed to save filter config.'
      },
      adminAuth: {
        loggedInAs: 'Current admin: {identity}',
        logout: 'Sign out current admin',
        notLoggedIn: 'No admin account signed in',
        login: 'Sign in admin account',
        expiresAt: 'Expires',
        timeUnknown: 'Unknown',
        logoutConfirm: {
          title: 'Sign out current admin?',
          description: 'After signing out you must sign in and pass admin verification again to view dashboard data.',
          confirm: 'Sign out',
          cancel: 'Cancel'
        },
        dataLocked: {
          title: 'Sign in an admin account first',
          description: 'Dashboard stats are only visible after signing in and verifying a site account with admin permission.'
        },
        modal: {
          title: 'Sign in admin account',
          subtitle: 'The dashboard requires a site account with admin permission.',
          close: 'Close',
          platformLabel: 'Platform',
          platform: {
            sub2api: 'Sub2API',
            newapi: 'New-API'
          },
          comingSoon: 'Coming soon',
          newApiPasswordOnly: 'New-API supports username & password login only.',
          siteUrlLabel: 'Site address (domain or IP)',
          siteUrlPlaceholder: 'e.g. https://sub.example.com or http://1.2.3.4:5555',
          methodLabel: 'Login method',
          method: {
            password: 'Email & Password',
            token: 'RT + AT'
          },
          fields: {
            emailPlaceholder: 'Admin email',
            usernamePlaceholder: 'Admin username',
            passwordPlaceholder: 'Admin password',
            accessTokenPlaceholder: 'Access Token (optional, can be empty)',
            refreshTokenPlaceholder: 'Refresh Token (required)',
            tokenTypePlaceholder: 'Token Type (optional, default Bearer)',
            tokenHelp: 'Just a Refresh Token is enough to sign in: the system refreshes once to get the latest expiry and auto-refreshes near expiry.'
          },
          submit: 'Sign in & verify',
          submitting: 'Verifying...'
        },
        errors: {
          request: 'Admin login request failed. Try again later.',
          missingCredentials: 'Fill in the site address and required fields for the selected method.',
          invalidUrl: 'Invalid site address. Enter a valid domain or IP and retry.',
          adminOnly: 'This account is not admin or verification failed. Check the credentials and retry.',
          network: 'Network or CORS request failed. Check the site URL.',
          platformUnsupported: 'Unsupported platform. Please choose Sub2API or New-API.',
          unknown: 'An unknown error occurred during admin login.'
        }
      }
    },
      upstream: {
        searchPlaceholder: 'Search site name...',
        addSite: 'Add Site',
        summary: '{connected} / {total} upstream sites connected',
        refresh: {
          action: 'Refresh Data',
          refreshing: 'Refreshing...',
          countdown: 'Refresh in {seconds}s',
          disabled: 'Auto refresh disabled'
        },
      modal: {
        title: 'Add Upstream Site',
        editTitle: 'Edit Upstream Site',
        cancel: 'Cancel',
        submit: 'Add Site',
        updateSubmit: 'Save Changes',
        submitting: 'Connecting...',
        form: {
          siteName: 'Site Name',
          siteNamePlaceholder: 'Enter site name',
          siteUrl: 'Site URL',
          siteUrlPlaceholder: 'Enter full site URL, e.g. https://api.example.com',
          platform: 'Platform',
          platforms: {
            auto: 'Auto Detect',
            sub2api: 'Sub2API',
            newapi: 'New-API'
          },
          authMode: 'Authentication Method',
          authModes: {
            password: 'Account Password',
            passwordHelp: 'Sign in with the site account and password, then save the session automatically.',
            token: 'Access Token / Refresh Token',
            tokenHelp: 'Use this for Sub2API sites where Cloudflare or two-factor login blocks direct password login.'
          },
          account: 'Account',
          accountPlaceholder: 'Enter account',
          password: 'Password',
          passwordPlaceholder: 'Enter password',
          passwordEditPlaceholder: 'Leave blank to keep current password',
          passwordEditHelp: 'Leaving this blank keeps the current login session. Enter a new password only when you want to re-login and update credentials.',
          accessToken: 'Access Token',
          accessTokenPlaceholder: 'Paste access_token, or leave blank and provide only refresh_token',
          refreshToken: 'Refresh Token',
          refreshTokenPlaceholder: 'Paste refresh_token; the server refreshes it once for the latest expiry',
          tokenType: 'Token Type',
          tokenTypePlaceholder: 'Defaults to Bearer',
          tokenHelp: 'When refresh_token is provided, the server refreshes it first to obtain the latest access_token and expiration time.',
          rechargeRate: 'Recharge Multiplier',
          rechargeRatePlaceholder: 'Enter the USD to CNY multiplier, e.g. 7',
          rechargeRateHelp: 'Required. CNY amount = USD amount × this multiplier.',
          remark: 'Remark',
          remarkPlaceholder: 'Enter remark (optional)'
        }
      },
      currency: {
        usdValue: '{amount} USD',
        cnyValue: '{amount} CNY'
      },
      fields: {
        siteName: 'Site Name',
        siteUrl: 'Site URL',
        platform: 'Platform',
        balance: 'Balance',
        todayConsume: 'Today Consume',
        historyRecharge: 'History Recharge',
        groupName: 'Group Name',
        groupMultiplier: 'Group Multiplier',
        availableGroups: 'Available Groups',
        viewAvailableGroups: 'View Available Groups',
        closeGroupsModal: 'Close',
        unknownPlatform: 'Unknown Platform',
        isConnected: 'Integration',
        connected: 'Connected',
        disconnected: 'Disconnected',
        lastUpdated: 'Last Updated',
        notSynced: 'Not synced yet'
      },
      status: {
        connecting: 'Connecting',
        syncing: 'Syncing',
        connected: 'Connected',
        error: 'Error'
      },
      empty: {
        title: 'No upstream sites found',
        description: 'Adjust your search or add an upstream site.'
      },
      delete: {
        action: 'Delete site',
        title: 'Delete this upstream site?',
        description: 'You are deleting "{name}". To restore it, you will need to add and log in again.',
        cancel: 'Cancel',
        confirm: 'Delete'
      },
      action: {
        sync: 'Sync',
        syncing: 'Syncing',
        edit: 'Edit Site',
        settings: 'Site Settings',
        actions: 'Actions'
      },
      siteSettings: {
        title: 'Site Alert Settings',
        balanceThreshold: 'Custom Balance Threshold',
        balanceThresholdHelp: 'Enable to use a site-specific threshold. Disable to use the global default.',
        balanceThresholdPlaceholder: 'Enter threshold amount',
        save: 'Save',
        saveSuccess: 'Saved',
        saving: 'Saving...',
        cancel: 'Cancel'
      },
      viewMode: {
        list: 'List Mode',
        card: 'Card Mode'
      },
      syncStream: {
        syncing: 'Syncing...',
        done: 'Sync complete',
        error: 'Sync failed',
      },
      errors: {
        invalidUrl: 'The site URL is invalid. Check it and try again.',
        network: 'Network or CORS request failed. Check the site URL and cross-origin settings.',
        auth: 'Login failed. Check the account or password.',
        request: 'The upstream API request failed. Try again later.',
        invalidResponse: 'The upstream response could not be parsed.',
        tokenMissing: 'Login succeeded but no access token was returned.',
        detect: 'The platform could not be auto-detected. Choose a platform and try again.',
        unknown: 'An unknown error occurred while connecting the upstream site.'
      }
    },
    groupRates: {
      badge: 'Rate Sync Ledger',
      title: 'Group Rates',
      subtitle: 'Review current upstream group multipliers and recent changes, then inspect multiplier history.',
      common: {
        placeholder: '—',
        allTypes: 'All Types',
        allPlatforms: 'All Platforms',
        unknown: 'Unknown'
      },
      platforms: {
        newapi: 'New-API',
        sub2api: 'Sub2API'
      },
      summary: {
        totalLabel: 'Total Group Rates',
        updatedLabel: 'Synced Records'
      },
      table: {
        title: 'Rate List',
        description: 'Rows preserve the ordering returned by the backend.'
      },
      fields: {
        siteName: 'Site Name',
        groupName: 'Group Name',
        type: 'Group Type',
        platform: 'Site Platform',
        currentMultiplier: 'Current Multiplier',
        delta: 'Rise/Fall',
        updatedAt: 'Updated Time',
        actions: 'Actions'
      },
      actions: {
        refresh: 'Refresh Data',
        createCampaign: 'Create Campaign',
        viewHistory: 'View History',
        viewHistoryForRate: 'View rise/fall history for {site} · {group}; current change {delta}',
        closeHistory: 'Close History',
        editType: 'Edit',
        closeEdit: 'Close Group Type Editor',
        connect: 'Click to Connect',
        closeConnect: 'Close connect dialog',
        saveConnect: 'Connect',
        cancel: 'Cancel',
        saveType: 'Save Type'
      },
      filters: {
        searchLabel: 'Search',
        searchPlaceholder: 'Search site or group...',
        typeLabel: 'Group Type',
        platformLabel: 'Site Platform'
      },
      sort: {
        label: 'Sort',
        multiplierAsc: 'Multiplier Low to High',
        multiplierDesc: 'Multiplier High to Low',
        siteNameAsc: 'Site Name A-Z',
        groupNameAsc: 'Group Name A-Z'
      },
      tabs: {
        all: 'All',
        mapped: 'Mapped',
        unmapped: 'Unmapped',
        deleted: 'Deleted'
      },
      pagination: {
        previous: 'Previous',
        next: 'Next',
        currentPage: 'Page {page} / {totalPages}',
        total: '{total} total',
        pageSize: '{pageSize} per page'
      },
      status: {
        loading: 'Loading group rates...',
        mapped: 'Connected',
        unmapped: 'Not Connected',
        deleted: 'Deleted'
      },
      empty: {
        title: 'No group rates yet',
        description: 'Synced upstream group multiplier data will appear here.'
      },
      history: {
        title: 'Multiplier History',
        titleWithGroup: '{site} · {group} Multiplier History',
        subtitle: 'Platform: {platform}',
        loading: 'Loading history records...',
        emptyTitle: 'No history records',
        emptyDescription: 'This site group has not returned multiplier history yet.',
        multiplier: 'Multiplier',
        delta: 'Rise/Fall',
        createdAt: 'Recorded Time'
      },
      edit: {
        title: 'Edit Group Type',
        titleWithGroup: 'Edit Group Type for {site} · {group}',
        description: 'Saving updates the multiplier type for this site group and refreshes the list.',
        typeLabel: 'Group Type',
        typePlaceholder: 'Select group type'
      },
      connect: {
        titleWithGroup: 'Connect {site} · {group}',
        description: 'Choose one of your site groups to add this upstream group to its mapping.',
        ownGroupLabel: 'My Site Groups',
        ownGroupPlaceholder: 'Select my site groups',
        upstreamGroupLabel: 'Connected Group',
        upstreamGroupPlaceholder: 'Select connected group',
        upstreamSiteLabel: 'Upstream Site',
        upstreamGroupNameLabel: 'Upstream Group',
        upstreamMultiplierLabel: 'Upstream Multiplier',
        upstreamPlatformLabel: 'Platform',
        modeData: 'Data Stats',
        modeReal: 'Real Connect',
        realDescription: 'This will automatically create an API key on the upstream site and a forwarding account on your admin site.',
        groupTypeLabel: 'Group Type',
        groupTypePlaceholder: 'Select group type',
        groupTypeOpenai: 'OpenAI',
        groupTypeAnthropic: 'Anthropic',
        groupTypeGemini: 'Gemini',
        groupTypeAntigravity: 'Antigravity',
        channelTypeLabel: 'Channel Type',
        channelTypePlaceholder: 'Select channel type',
        realNotSupported: 'Real connect is not supported for this platform',
        realConnecting: 'Creating connection...',
        realSuccess: 'Real connection created successfully',
        realFailed: 'Failed to create real connection',
        modeBind: 'Manual Bind',
        bindDescription: 'Select an existing upstream Key to bind to this group without creating new resources.',
        bindSelectKey: 'Select Upstream Key',
        bindKeysLoading: 'Loading key list...',
        bindKeysEmpty: 'No keys available for this site',
        bindFailed: 'Failed to bind'
      },
      disconnect: {
        action: 'Disconnect',
        title: 'Disconnect',
        description: 'Confirm disconnecting {site} · {group}?',
        unlinkOnly: 'Unlink Only',
        unlinkOnlyHint: 'Only remove the local binding record, keep the upstream Key and Admin account',
        deleteAll: 'Delete Account & Key',
        deleteAllHint: 'Also delete the upstream Key and the Admin site forwarding account',
        confirm: 'Confirm',
        disconnecting: 'Disconnecting...',
        failed: 'Failed to disconnect'
      },
      format: {
        multiplier: '{value}x',
        deltaMultiplier: '{value}x'
      },
      errors: {
        network: 'Network or CORS request failed. Check the API URL and cross-origin settings.',
        request: 'The group rates API request failed. Try again later.',
        unknown: 'An unknown error occurred while loading group rates.'
      }
    },
    groupRateCampaigns: {
      title: 'Rate Campaigns',
      subtitle: 'Batch-adjust your own group multipliers, with scheduled start/end and automatic restore.',
      common: {
        placeholder: '—'
      },
      actions: {
        create: 'New Campaign',
        refresh: 'Refresh',
        start: 'Start Now',
        end: 'End Campaign',
        cancel: 'Cancel Campaign',
        viewDetail: 'View Details',
        close: 'Close',
        preview: 'Preview Impact',
        confirmCreate: 'Create Campaign',
        cancelEdit: 'Cancel'
      },
      tabs: {
        all: 'All'
      },
      status: {
        draft: 'Draft',
        scheduled: 'Scheduled',
        running: 'Running',
        ending: 'Ending',
        ended: 'Ended',
        partial: 'Partial',
        failed: 'Failed',
        cancelled: 'Cancelled',
        loading: 'Loading campaigns...'
      },
      fields: {
        name: 'Campaign Name',
        status: 'Status',
        startAt: 'Start Time',
        endAt: 'End Time',
        summary: 'Result',
        createdBy: 'Created By',
        actions: 'Actions'
      },
      empty: {
        title: 'No campaigns yet',
        description: 'Click "New Campaign" to create your first rate campaign.'
      },
      pagination: {
        total: '{total} total',
        pageSize: '{pageSize} per page',
        currentPage: 'Page {page} / {totalPages}',
        previous: 'Previous',
        next: 'Next'
      },
      format: {
        summary: '{applied}/{total} applied'
      },
      errors: {
        network: 'Network or CORS request failed. Check the API URL and cross-origin settings.',
        request: 'The rate campaigns API request failed. Try again later.',
        unknown: 'An unknown error occurred while loading rate campaigns.',
        emptySelection: 'Select at least one group, and every group must exist in your own groups.',
        invalidName: 'Invalid campaign name. It must be between 1 and 80 characters.',
        invalidAdjustment: 'Invalid campaign multiplier. Check that every selected group has a valid fixed rate.',
        invalidSchedule: 'Invalid schedule. Please check the start/end time settings.',
        noNotifyBots: 'Select at least one bot when notifications are enabled.',
        notFound: 'Campaign not found.',
        invalidState: 'This action is not allowed for the campaign\'s current status.',
        duplicateGroup: 'The same group cannot be selected more than once.'
      },
      editor: {
        titleCreate: 'New Rate Campaign',
        sectionInfo: 'Campaign Info',
        nameLabel: 'Campaign Name',
        namePlaceholder: 'e.g. Double 11 Sale',
        descriptionLabel: 'Description',
        descriptionPlaceholder: 'Optional, for your own reference',
        sectionSelection: 'Select Groups',
        selectionHint: 'Set a campaign multiplier for each group',
        groupsEmpty: 'No groups available',
        groupMultiplierPlaceholder: 'Campaign multiplier',
        sectionSchedule: 'Schedule',
        startModeLabel: 'Start Mode',
        startNow: 'Start Now',
        startScheduled: 'Scheduled Start',
        startDraft: 'Save as Draft',
        startAtLabel: 'Start Time',
        endModeLabel: 'End Mode',
        endScheduled: 'Scheduled End',
        endManual: 'Manual End',
        endAtLabel: 'End Time',
        sectionNotify: 'Notifications',
        notifyEnableLabel: 'Enable Bot Notifications',
        notifyBotSelectLabel: 'Select Bots',
        notifyNoBots: 'No bots available. Configure one in system settings first.',
        notifyStartTemplateLabel: 'Start Notification Text',
        notifyEndTemplateLabel: 'End Notification Text',
        notifyVariablesTitle: 'Available variables, click to copy',
        notifyVarActivityName: 'Campaign name',
        notifyVarTotalCount: 'Total target groups',
        notifyVarAppliedCount: 'Applied count',
        notifyVarFailedCount: 'Failed count',
        notifyVarStartTime: 'Start time',
        notifyVarEndTime: 'End time',
        copyVarFailed: 'Copy failed. Please copy the variable manually.',
        previewTitle: 'Preview Affected Groups',
        previewEmpty: 'No preview yet. Click "Preview Impact" to see affected groups.',
        previewGroupName: 'Group Name',
        previewOriginal: 'Original',
        previewCampaign: 'Campaign',
        previewTotal: '{total} groups affected',
        errors: {
          nameRequired: 'Please enter a campaign name',
          selectionEmpty: 'Please select at least one group',
          groupMultiplierInvalid: 'Enter a valid campaign multiplier for every group',
          scheduleInvalid: 'Please check the start/end time settings',
          notifyBotsRequired: 'Please select at least one bot when notifications are enabled'
        }
      },
      detail: {
        title: 'Campaign Details',
        sectionConfig: 'Campaign Config',
        sectionItems: 'Group Details',
        itemGroupName: 'Group Name',
        itemOriginal: 'Original',
        itemCampaign: 'Campaign',
        itemRestored: 'Restored',
        itemApplyStatus: 'Start Status',
        itemRestoreStatus: 'Restore Status',
        noReason: '—',
        confirmEnd: 'Are you sure you want to manually end this campaign? All groups will be restored to their original multipliers immediately.',
        confirmCancel: 'Are you sure you want to cancel this campaign? No pricing changes will be applied.'
      }
    },
    mySites: {
      errors: {
        invalidAutoPricingConfig: 'Invalid auto-pricing config: primary upstream not in linked upstreams, or min multiplier exceeds max.'
      }
    },
    settings: {
      title: 'System Settings',
      subtitle: 'Manage system parameters, notification channels, and automation strategies.',
      save: 'Save Settings',
      saving: 'Saving...',
      saveSuccess: 'Saved',
      strategyDescription: 'Configure data refresh interval, alert thresholds, and automation strategies.',
      requiresRefresh: 'Enable data refresh interval first so the system can detect changes automatically.',
      balanceWarningAmount: 'Trigger Amount (CNY)',
      notifyBots: 'Send Notifications To',
      customTemplate: 'Custom Template',
      balanceTemplateVars: '(Variables: {siteName}, {balance}, {threshold})',
      multiplierTemplateVars: '(Variables: {siteName}, {groupName}, {oldRate}, {newRate}, {changeDirection})',
      unnamedBot: 'Unnamed Bot',
      noBotsConfigured: 'Configure bots in "Channels & Alerts" first',
      mustSelectBot: 'At least one notification bot must be selected',
      varSiteName: 'Site name',
      varBalance: 'Current balance (CNY)',
      varThreshold: 'Threshold (CNY)',
      varGroupName: 'Group name',
      varOldRate: 'Old rate',
      varNewRate: 'New rate',
      varChangeDirection: 'Change direction',
      pricingAmount: 'Adjustment Amount',
      botNameLabel: 'Bot Name',
      botNameDingtalkPlaceholder: 'e.g., DingTalk Main',
      botNameFeishuPlaceholder: 'e.g., Feishu Main',
      botNameTelegramPlaceholder: 'e.g., TG Main',
      addDingtalkBot: 'Add DingTalk Bot',
      addFeishuBot: 'Add Feishu Bot',
      addTelegramBot: 'Add TG Bot',
      emptyDingtalk: 'No DingTalk bots configured',
      emptyFeishu: 'No Feishu bots configured',
      emptyTelegram: 'No Telegram bots configured',
      tabs: {
        strategy: 'Strategy & Automation',
        channels: 'Channels & Alerts',
        templates: 'Message Templates'
      },
      sections: {
        basic: {
          title: 'Basic Settings',
          description: 'Configure basic system operation parameters.',
          refreshInterval: 'Data Refresh Interval',
          refreshIntervalHelp: 'Set the interval for automatically fetching upstream site data. Minimum 60 seconds.',
          seconds: 'Seconds'
        },
        thresholds: {
          title: 'Site Warning Thresholds',
          description: 'Configure default alarm triggers for all upstream sites.',
          balanceWarning: 'Balance Warning',
          balanceWarningHelp: 'Send an alert when an upstream site\'s balance (converted to CNY via recharge rate) falls below the configured amount.',
          multiplierChangeWarning: 'Multiplier Change Alert',
          multiplierChangeWarningHelp: 'Send notifications when any mapped group multiplier changes.'
        },
        pricing: {
          title: 'Auto-Pricing',
          description: 'Configure auto-adjustment rules when an upstream mapped group multiplier increases.',
          enableAutoPricing: 'Auto-Pricing',
          enableAutoPricingHelp: 'Automatically adjust "Own Group" multiplier when an upstream group multiplier increases.',
          strategy: 'Adjustment Strategy',
          strategyFixed: 'Fixed Increase (+)',
          strategyPercentage: 'Percentage Increase (%)',
          fixedValuePlaceholder: 'e.g., 0.1',
          percentageValuePlaceholder: 'e.g., 10'
        },
        channels: {
          title: 'Notification Channels',
          description: 'Configure third-party channels for receiving system alerts (e.g., DingTalk, Telegram, Feishu).',
          dingtalk: 'DingTalk Bot',
          dingtalkHelp: 'Configure Webhook and Secret for DingTalk group bot.',
          feishu: 'Feishu Bot',
          feishuHelp: 'Configure Webhook and Secret for Feishu group bot.',
          telegram: 'Telegram Bot',
          telegramHelp: 'Configure Bot Token and Chat ID for Telegram.',
          webhookUrl: 'Webhook URL',
          secret: 'Secret',
          botToken: 'Bot Token',
          chatId: 'Chat ID',
          proxyUrl: 'Proxy URL (Optional)',
          proxyUrlPlaceholder: 'e.g. http://127.0.0.1:7890',
          proxyUrlHelp: 'Use a proxy when the server cannot reach Telegram directly. Leave blank for direct connection.',
          loading: 'Loading notification channel settings...',
          testConnection: 'Test Connection',
          testConnectionSuccess: 'Sent Successfully'
        },
        templates: {
          balanceTemplatePlaceholder: 'e.g., [Warning] {siteName} balance (CNY) is below {threshold}, current is {balance}.',
          multiplierTemplatePlaceholder: 'e.g., [Rate Change] {siteName} {groupName} changed from {oldRate}x to {newRate}x.'
        }
      },
      errors: {
        network: 'Network or CORS request failed. Check the API URL and cross-origin settings.',
        request: 'The notification channel test request failed. Try again later.',
        unknown: 'An unknown error occurred while testing the notification channel.',
        invalidChannel: 'The notification channel type is invalid.',
        missingWebhook: 'Enter the robot webhook URL first.',
        missingTelegramConfig: 'Enter the Telegram Bot Token and Chat ID first.',
        sendFailed: 'Failed to send the test message. Check the robot configuration and network connectivity.'
      }
    },
    system: {
      version: 'Version {version}',
      errors: {
        network: 'System info request failed. Check your network connection.',
        request: 'System request failed. Please try again later.'
      }
    }
  }
}
