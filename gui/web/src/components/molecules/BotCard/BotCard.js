import React, { Component } from 'react';
import PropTypes from 'prop-types';
import Pill from '../../atoms/Pill/Pill';
import RunStatus from '../../atoms/RunStatus/RunStatus';
// import chartThumb from '../../../assets/images/chart-thumb.png';
import styles from './BotCard.module.scss';
import PillGroup from '../PillGroup/PillGroup';
import StartStop from '../../atoms/StartStop/StartStop';
import BotExchangeInfo from '../../atoms/BotExchangeInfo/BotExchangeInfo';
import BotAssetsInfo from '../../atoms/BotAssetsInfo/BotAssetsInfo';
import BotBidAskInfo from '../../atoms/BotBidAskInfo/BotBidAskInfo';
import Button from '../../atoms/Button/Button';
import Constants from '../../../Constants';
import PopoverMenu from '../PopoverMenu/PopoverMenu';

import start from '../../../kelp-ops-api/start';
import stop from '../../../kelp-ops-api/stop';
import deleteBot from '../../../kelp-ops-api/deleteBot';
import getState from '../../../kelp-ops-api/getState';
import getBotInfo from '../../../kelp-ops-api/getBotInfo';

let defaultBotInfo = {
  "last_updated": "Never",
  "strategy": "buysell",
  "is_testnet": true,
  "trading_pair": {
    "Base": "?",
    "Quote": "?"
  },
  "asset_base": {
    "asset_type": "credit_alphanum4",
    "asset_code": "?",
    "asset_issuer": "?"
  },
  "asset_quote": {
    "asset_type": "credit_alphanum4",
    "asset_code": "?",
    "asset_issuer": "?"
  },
  "balance_base": -1,
  "balance_quote": -1,
  "num_bids": -1,
  "num_asks": -1,
  "spread_value": "?",
  "spread_pct": "?",
}

// botStateIntervalMillis: it's inexpensive to call this since it only looks in-memory on the backend so
// we can run it once every second
const botStateIntervalMillis = 1000;
// botInfoIntervalMillis: its expensive to run this frequently because it calls out to horizon which will
// consume the rate limit, 5 seconds is fast enough since that's the ledger close time on SDEX
const botInfoIntervalMillis = 5000;
// botInfoTimeoutMillis: should be less than botInfoIntervalMillis
const botInfoTimeoutMillis = 3000;

class BotCard extends Component {
  constructor(props) {
    super(props);
    this.state = {
      timeStarted: null,
      timeElapsed: null,
      popoverVisible: false,
      state: Constants.BotState.initializing,
      botInfo: defaultBotInfo,
    };

    this.toggleBot = this.toggleBot.bind(this);
    this.checkState = this.checkState.bind(this);
    this.checkBotInfo = this.checkBotInfo.bind(this);
    this.startBot = this.startBot.bind(this);
    this.stopBot = this.stopBot.bind(this);
    this.tick = this.tick.bind(this);
    this.toggleOptions = this.toggleOptions.bind(this);
    this.closeOptions = this.closeOptions.bind(this);
    this.handleClick = this.handleClick.bind(this);
    this.editBot = this.editBot.bind(this);
    this.showDetails = this.showDetails.bind(this);
    this.showOffers = this.showOffers.bind(this);
    this.showMarket = this.showMarket.bind(this);
    this.callDeleteBot = this.callDeleteBot.bind(this);

    this._asyncRequests = {};
  }

  static propTypes = {
    name: PropTypes.string.isRequired,
    enablePubnetBots: PropTypes.bool.isRequired,
    baseUrl: PropTypes.string.isRequired,
    addError: PropTypes.func.isRequired,
    errorLevelInfoForBot: PropTypes.array.isRequired,
    errorLevelWarningForBot: PropTypes.array.isRequired,
    errorLevelErrorForBot: PropTypes.array.isRequired,
    setModal: PropTypes.func.isRequired,
  };

  checkState() {
    if (!this._asyncRequests["state"]) {
      var _this = this;
      this._asyncRequests["state"] = getState(this.props.baseUrl, this.props.name).then(resp => {
        if (!_this._asyncRequests["state"]) {
          // if it has been deleted it means we don't want to process the result
          return
        }

        delete _this._asyncRequests["state"];
        let state = resp.trim();
        if (_this.state.state !== state) {
          _this.setState({
            state: state,
          });
        }
      });
    }
  }

  checkBotInfo() {
    const controller = new AbortController();
    if (!this._asyncRequests["botInfo"]) {
      var _this = this;
      this._asyncRequests["botInfo"] = getBotInfo(this.props.baseUrl, this.props.name, controller.signal).then(resp => {
        if (!_this._asyncRequests["botInfo"]) {
          // if it has been deleted it means we don't want to process the result
          return
        }

        delete _this._asyncRequests["botInfo"];
        if (resp.kelp_error) {
          this.props.addError(resp.kelp_error);
        } else if (JSON.stringify(resp) !== "{}") {
          _this.setState({
            botInfo: resp,
          });
        } else {
          _this.setState({
            botInfo: defaultBotInfo,
          });
        }
      }).catch(function(error) {
        delete _this._asyncRequests["botInfo"];
        console.error(error);
      });

      // set a timeout on the fetch request
      if (this._asyncRequests["botInfo"]) {
        setTimeout(controller.abort.bind(controller), botInfoTimeoutMillis);
      }
    }
  }

  componentWillMount() {
    document.addEventListener('mousedown', this.handleClick, false);
  }

  handleClick(e) {
    if (!this.optionsWrapperNode) {
      return;
    }

    if (this.optionsWrapperNode.contains(e.target)) {
      // click is inside the options wrapper node, so nothing extra to do
      return;
    }
    this.closeOptions();
  }

  componentDidMount() {
    this.checkState();
    this.checkBotInfo();
    this._stateTimer = setInterval(this.checkState, botStateIntervalMillis);
    this._infoTimer = setInterval(this.checkBotInfo, botInfoIntervalMillis);
  }

  componentWillUnmount() {
    document.removeEventListener('mousedown', this.handleClick, false);

    if (this._stateTimer) {
      clearTimeout(this._stateTimer);
      this._stateTimer = null;
    }

    if (this._infoTimer) {
      clearTimeout(this._infoTimer);
      this._infoTimer = null;
    }

    if (this._tickTimer) {
      clearTimeout(this._tickTimer);
      this._tickTimer = null;
    }

    if (this._asyncRequests["state"]) {
      delete this._asyncRequests["state"];
    }

    if (this._asyncRequests["start"]) {
      delete this._asyncRequests["start"];
    }
    
    if (this._asyncRequests["stop"]) {
      delete this._asyncRequests["stop"];
    }

    if (this._asyncRequests["delete"]) {
      delete this._asyncRequests["delete"];
    }

    if (this._asyncRequests["botInfo"]) {
      delete this._asyncRequests["botInfo"];
    }
  }

  toggleBot() {
    if (this.state.state === Constants.BotState.running) {
      this.stopBot();
    } else {
      this.startBot();
    }
    this.checkState();
    this.checkBotInfo();
  }

  startBot() {
    var _this = this;
    this._asyncRequests["start"] = start(this.props.baseUrl, this.props.name).then(resp => {
      if (!_this._asyncRequests["start"]) {
        // if it has been deleted it means we don't want to process the result
        return
      }

      delete _this._asyncRequests["start"];

      if (resp.kelp_error) {
        this.props.addError(resp.kelp_error);
      } else {
        _this.setState({
          timeStarted: new Date(),
        }, () => {
          _this.checkState();
          _this.checkBotInfo();
          _this.tick();
          _this._tickTimer = setInterval(_this.tick, 1000);
        });
      }
    });
  }

  stopBot() {
    var _this = this;
    this._asyncRequests["stop"] = stop(this.props.baseUrl, this.props.name).then(resp => {
      if (!_this._asyncRequests["stop"]) {
        // if it has been deleted it means we don't want to process the result
        return
      }

      delete _this._asyncRequests["stop"];
      _this.setState({
        timeStarted: null,
      });
      clearTimeout(_this._tickTimer);
      _this._tickTimer = null;
    });
  }

  callDeleteBot() {
    var _this = this;
    this._asyncRequests["delete"] = deleteBot(this.props.baseUrl, this.props.name).then(resp => {
      if (!_this._asyncRequests["delete"]) {
        // if it has been deleted it means we don't want to process the result
        return
      }

      delete _this._asyncRequests["delete"];
      clearTimeout(_this._tickTimer);
      _this._tickTimer = null;
      // reload parent view
      _this.props.reload();
    });
    
    this.setState({
      popoverVisible: false,
    });
  }

  tick() {
    let timeNow = new Date();
    let diffTime = timeNow - this.state.timeStarted;
    let elapsed = new Date(diffTime);
    this.setState({
      timeElapsed: elapsed,
    });
  }

  toggleOptions() {
    this.setState({
      popoverVisible: !this.state.popoverVisible,
    })
  }

  closeOptions() {
    this.setState({
      popoverVisible: false,
    })
  }

  editBot() {
    this.props.history.push('/edit?bot_name=' + encodeURIComponent(this.props.name))
  }

  showDetails() {
    this.props.history.push('/details?bot_name=' + encodeURIComponent(this.props.name))
  }

  showOffers() {
    const tradingAccount = this.state.botInfo.trading_account;
    let urlNetwork = "testnet";
    if (!this.state.botInfo.is_testnet) {
      urlNetwork = "public";
    }
    
    const link = "https://stellar.expert/explorer/" + urlNetwork + "/account/" + tradingAccount + "?filter=active-offers";
    window.open(link);
  }

  showMarket() {
    let baseCode = this.state.botInfo.asset_base.asset_type === "native" ? "XLM/native" : this.state.botInfo.asset_base.asset_code + "/" + this.state.botInfo.asset_base.asset_issuer;
    let quoteCode = this.state.botInfo.asset_quote.asset_type === "native" ? "XLM/native" : this.state.botInfo.asset_quote.asset_code + "/" + this.state.botInfo.asset_quote.asset_issuer;
    let link = "https://testnet.interstellar.exchange/app/#/trade/guest/" + baseCode + "/" + quoteCode;
    if (!this.state.botInfo.is_testnet) {
      link = "https://interstellar.exchange/app/#/trade/guest/" + baseCode + "/" + quoteCode;
    }
    window.open(link);
  }

  render() {
    if (!this.props.enablePubnetBots && !this.state.botInfo.is_testnet) {
      // don't show pubnet bots when running a testnet only version
      return "";
    }

    let popover = "";
    if (this.state.popoverVisible) {
      let enableEdit = this.state.state === Constants.BotState.stopped || this.state.state === Constants.BotState.stopping;
      popover = (
        <div>
          <div className={styles.optionsSpacer}/>
          <PopoverMenu
            className={styles.optionsMenu}
            alwaysOnClick={this.closeOptions}
            enableOffers={true}
            onOffers={this.showOffers}
            enableMarket={true}
            onMarket={this.showMarket}
            enableEdit={enableEdit}
            onEdit={this.editBot}
            enableCopy={false}
            onCopy={this.toggleOptions}
            enableDelete={true}
            onDelete={this.callDeleteBot}
          />
        </div>
      );
    }

    return (
      <div className={styles.card}>
        <span className={this.state.state === Constants.BotState.running ? styles.statusRunning : styles.statusStopped}/>

        <div className={styles.optionsWrapper} ref={node => this.optionsWrapperNode = node}>
          <Button
              eventName="bot-menu"
              icon="options"
              size="large"
              variant="transparent"
              hsize="round"
              className={styles.optionsTrigger}
              onClick={this.toggleOptions}
          />
          {popover}
        </div>

        {/* <div className={styles.sortingArrows}>
          <Button
              eventName="bot-sortup"
              icon="chevronUp"
              variant="transparent"
              hsize="round"
          />
          <Button
              eventName="bot-sortdown"
              icon="chevronDown"
              variant="transparent"
              hsize="round"
          />
        </div> */}

        <div className={styles.firstColumn}>
          <h2 className={styles.title}><span onClick={this.showDetails}>{this.props.name}</span></h2>
          <div className={styles.botDetailsLine}>
            <BotExchangeInfo
              isTestnet={this.state.botInfo.is_testnet}
              strategy={this.state.botInfo.strategy}
              />
          </div>
          <div>
            <BotAssetsInfo
              assetBaseCode={this.state.botInfo.trading_pair.Base}
              assetBaseIssuer={this.state.botInfo.asset_base.asset_issuer}
              assetBaseBalance={this.state.botInfo.balance_base}
              assetQuoteCode={this.state.botInfo.trading_pair.Quote}
              assetQuoteIssuer={this.state.botInfo.asset_quote.asset_issuer}
              assetQuoteBalance={this.state.botInfo.balance_quote}
              lastUpdated={this.state.botInfo.last_updated}
            />
          </div>
        </div>

        <div className={styles.secondColumn}>
          <div className={styles.notificationsLine}>
            <PillGroup>
              <Pill errors={this.props.errorLevelInfoForBot} type="info" onClick={() => { this.props.setModal("info", this.props.errorLevelInfoForBot) }} />
              <Pill errors={this.props.errorLevelWarningForBot} type="warning" onClick={() => { this.props.setModal("warning", this.props.errorLevelWarningForBot) }} />
              <Pill errors={this.props.errorLevelErrorForBot} type="error" onClick={() => { this.props.setModal("error", this.props.errorLevelErrorForBot) }} />
            </PillGroup>
          </div>
          <BotBidAskInfo
            spread_value={this.state.botInfo.spread_value}
            spread_pct={this.state.botInfo.spread_pct}
            num_bids={this.state.botInfo.num_bids}
            num_asks={this.state.botInfo.num_asks}
          />
        </div>

        {/* <div className={styles.thirdColumn}>
          <img className={styles.chartThumb} src={chartThumb} alt="chartThumb"/>
        </div> */}

        <div className={styles.fourthColumn}>
          <RunStatus 
            className={styles.statusDetails}
            state={this.state.state}
            timeRunning={this.state.timeElapsed}
          />
          <StartStop
            onClick={this.toggleBot} 
            state={this.state.state}
          />
        </div>

      </div>
    );
  }
}

export default BotCard;