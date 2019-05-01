import React, { Component } from 'react';
import styles from'./ReloadButton.module.scss';
import Button from '../Button/Button';


class ReloadButton extends Component {
  render() {
    return (
      <Button 
        icon="refresh"
        className={styles.button}
        variant="transparent"
        hsize="round"
        >
      </Button>
    );
  }
}

export default ReloadButton;